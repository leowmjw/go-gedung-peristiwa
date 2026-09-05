package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	Store      StoreConfig
	PrefixID   string
	FlushEvery time.Duration // zero disables timed background flush
	Retention  bool          // enable change-feed retention via maintenance (Store.ChangeFeedRetainFor)
	RunMaint   bool          // start Maintenance.Run in a background goroutine
	MaintCtx   context.Context
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
	wOpts.OnFlushError = func(err error) {
		// Terminal: a background flush failed once and the writer is now
		// permanently unusable (ErrWriterFailed on every later call). The
		// common cause here is fencing — a newer process opened a writer on
		// this same prefix (rolling deploy overlap) — but any terminal cause
		// lands here. Callers must stop writing; Close still runs every
		// remaining shutdown step regardless of this error.
		slog.Warn("isledb: writer failed, no longer accepting writes", "prefix", cfg.PrefixID, "error", err)
	}
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
		retention.RetainFor = NormalizeChangeFeedRetainFor(cfg.Store.ChangeFeedRetainFor)
		mOpts.ChangeFeedRetention = &retention
	}
	mOpts.OnError = func(err error) {
		slog.Warn("isledb: maintenance cycle failed", "prefix", cfg.PrefixID, "error", err)
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
			if err := maintenance.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
				// Maintenance ownership is fenced through the object store: if a
				// newer process (e.g. the incoming pod in a rolling deploy) opens
				// Maintenance on this same prefix, Run returns here instead of
				// looping forever. That's expected during a rollout overlap, so
				// we log and stop rather than treating it as fatal — see
				// "Rolling deploys / fencing" in AGENTS.md.
				slog.Warn("isledb: maintenance stopped", "prefix", cfg.PrefixID, "error", err)
			}
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

// Close shuts down handles in order, continuing through every step even if
// an earlier one fails, and returns the first error seen. A failure here is
// not just "external service down": Writer/Maintenance ownership is fenced
// through the object store, so a newer process opening the same prefix (a
// rolling Kubernetes deploy with the old pod still draining, for example)
// makes the old writer/maintenance terminal — Flush and Maintenance.Run then
// fail by design. Returning early on that first error used to skip closing
// everything after it (leaking the reader, DB handle, and bucket on every
// fenced shutdown); see AGENTS.md "Rolling deploys / fencing".
func (p *PrefixDB) Close(ctx context.Context) error {
	if p.maintCancel != nil {
		p.maintCancel()
	}
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	if p.Writer != nil {
		keep(p.Writer.Flush(ctx))
	}
	if p.Maintenance != nil {
		_, _ = p.Maintenance.RunOnce(ctx)
		keep(p.Maintenance.Close(ctx))
	}
	if p.Writer != nil {
		keep(p.Writer.Close(ctx))
	}
	if p.Reader != nil {
		keep(p.Reader.Close())
	}
	if p.DB != nil {
		keep(p.DB.Close())
	}
	if p.closeBucket != nil {
		keep(p.closeBucket())
	}
	return first
}
