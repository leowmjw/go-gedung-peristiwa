package pipeline

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ankur-anand/isledb"
	"gocloud.dev/blob/memblob"
)

const DefaultCacheRoot = "data/cache/gedung-peristiwa"

// PrefixDB is one IsleDB database prefix with writer, reader, and maintenance handles.
type PrefixDB struct {
	ID          string
	Prefix      string
	DB          *isledb.DB
	Writer      *isledb.Writer
	Reader      *isledb.Reader
	Maintenance *isledb.Maintenance
	CacheDir    string

	closeBucket func() error
	maintCancel context.CancelFunc
}

// PrefixOpenConfig controls opening a prefix database.
type PrefixOpenConfig struct {
	Store       StoreConfig
	PrefixID    string
	FlushEvery  time.Duration // zero disables timed background flush
	Retention   bool          // enable default change-feed retention via maintenance
	RunMaint    bool          // start Maintenance.Run in a background goroutine
	MaintCtx    context.Context
}

func defaultDBOptions(prefix string) isledb.DBOptions {
	return isledb.DBOptions{
		Prefix: prefix,
		ChangeFeed: &isledb.ChangeFeedOptions{
			Payload: isledb.ChangeFeedFullValues,
		},
	}
}

func openIsleDB(ctx context.Context, cfg StoreConfig, prefixID string) (*isledb.DB, func() error, error) {
	prefix := tenantPrefix(prefixID, cfg.PrefixRoot)
	opts := defaultDBOptions(prefix)

	switch cfg.Backend {
	case BackendMemory:
		bkt := memblob.OpenBucket(nil)
		db, err := isledb.OpenBucket(ctx, bkt, "memory", opts)
		if err != nil {
			_ = bkt.Close()
			return nil, nil, err
		}
		return db, func() error { return bkt.Close() }, nil
	case BackendMinIO:
		ensureS3Env(cfg)
		if err := ensureBucket(ctx, cfg); err != nil {
			return nil, nil, fmt.Errorf("ensure bucket: %w", err)
		}
		url := minioBucketURL(cfg.MinBucket, cfg.MinEndpoint)
		db, err := isledb.Open(ctx, url, opts)
		return db, nil, err
	case BackendTigris:
		ensureS3Env(cfg)
		if err := ensureBucket(ctx, cfg); err != nil {
			return nil, nil, fmt.Errorf("ensure bucket: %w", err)
		}
		url := tigrisBucketURL(cfg.TigrisBucket)
		db, err := isledb.Open(ctx, url, opts)
		return db, nil, err
	default:
		return nil, nil, fmt.Errorf("unknown backend %q", cfg.Backend)
	}
}

// OpenPrefixDB opens writer, reader, and maintenance for one prefix.
func OpenPrefixDB(ctx context.Context, cfg PrefixOpenConfig) (*PrefixDB, error) {
	db, closeBucket, err := openIsleDB(ctx, cfg.Store, cfg.PrefixID)
	if err != nil {
		return nil, fmt.Errorf("open db %s: %w", cfg.PrefixID, err)
	}

	cache := cacheDir(cfg.Store, cfg.PrefixID)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		_ = db.Close()
		if closeBucket != nil {
			_ = closeBucket()
		}
		return nil, err
	}

	wOpts := isledb.DefaultWriterOptions()
	wOpts.Flush.Interval = cfg.FlushEvery
	writer, err := db.OpenWriter(ctx, wOpts)
	if err != nil {
		_ = db.Close()
		if closeBucket != nil {
			_ = closeBucket()
		}
		return nil, fmt.Errorf("open writer %s: %w", cfg.PrefixID, err)
	}

	reader, err := db.OpenReader(ctx, isledb.DefaultReaderOpenOptions(cache))
	if err != nil {
		_ = writer.Close(ctx)
		_ = db.Close()
		if closeBucket != nil {
			_ = closeBucket()
		}
		return nil, fmt.Errorf("open reader %s: %w", cfg.PrefixID, err)
	}

	mOpts := isledb.DefaultMaintenanceOptions()
	if cfg.Retention {
		retention := isledb.DefaultChangeFeedRetentionOptions()
		mOpts.ChangeFeedRetention = &retention
	}
	maintenance, err := db.OpenMaintenance(ctx, mOpts)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close(ctx)
		_ = db.Close()
		if closeBucket != nil {
			_ = closeBucket()
		}
		return nil, fmt.Errorf("open maintenance %s: %w", cfg.PrefixID, err)
	}

	pdb := &PrefixDB{
		ID:          cfg.PrefixID,
		Prefix:      tenantPrefix(cfg.PrefixID, cfg.Store.PrefixRoot),
		DB:          db,
		Writer:      writer,
		Reader:      reader,
		Maintenance: maintenance,
		CacheDir:    cache,
		closeBucket: closeBucket,
	}

	if cfg.RunMaint {
		maintCtx := cfg.MaintCtx
		if maintCtx == nil {
			maintCtx = ctx
		}
		runCtx, cancel := context.WithCancel(maintCtx)
		pdb.maintCancel = cancel
		go func() {
			_ = maintenance.Run(runCtx)
		}()
	}

	return pdb, nil
}

// OpenReaderDB opens a reader-only database handle (no writer/maintenance).
func OpenReaderDB(ctx context.Context, cfg StoreConfig, prefixID string) (*isledb.DB, *isledb.Reader, func() error, error) {
	db, closeBucket, err := openIsleDB(ctx, cfg, prefixID)
	if err != nil {
		return nil, nil, nil, err
	}

	cache := cacheDir(cfg, prefixID)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		_ = db.Close()
		if closeBucket != nil {
			_ = closeBucket()
		}
		return nil, nil, nil, err
	}

	reader, err := db.OpenReader(ctx, isledb.DefaultReaderOpenOptions(cache))
	if err != nil {
		_ = db.Close()
		if closeBucket != nil {
			_ = closeBucket()
		}
		return nil, nil, nil, err
	}

	closer := func() error {
		if err := reader.Close(); err != nil {
			return err
		}
		if err := db.Close(); err != nil {
			return err
		}
		if closeBucket != nil {
			return closeBucket()
		}
		return nil
	}
	return db, reader, closer, nil
}

// RunMaintenanceOnce performs one bounded maintenance pass while the writer stays open.
func (p *PrefixDB) RunMaintenanceOnce(ctx context.Context) error {
	_, err := p.Maintenance.RunOnce(ctx)
	return err
}

// Close shuts down handles in the correct order.
func (p *PrefixDB) Close(ctx context.Context) error {
	if p.maintCancel != nil {
		p.maintCancel()
	}
	if p.Writer != nil {
		if err := p.Writer.Flush(ctx); err != nil {
			return err
		}
	}
	if p.Maintenance != nil {
		_, _ = p.Maintenance.RunOnce(ctx)
		if err := p.Maintenance.Close(ctx); err != nil {
			return err
		}
	}
	if p.Writer != nil {
		if err := p.Writer.Close(ctx); err != nil {
			return err
		}
	}
	if p.Reader != nil {
		if err := p.Reader.Close(); err != nil {
			return err
		}
	}
	if p.DB != nil {
		if err := p.DB.Close(); err != nil {
			return err
		}
	}
	if p.closeBucket != nil {
		return p.closeBucket()
	}
	return nil
}
