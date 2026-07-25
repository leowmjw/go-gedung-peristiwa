package pipeline

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ankur-anand/isledb"
	"github.com/ankur-anand/isledb/blobstore"

	"github.com/leow/go-gedung-peristiwa/internal/eventgen"
	"github.com/leow/go-gedung-peristiwa/internal/model"
)

// TenantPipeline is one IsleDB prefix (single writer).
type TenantPipeline struct {
	ID       string
	store    *blobstore.Store
	db       *isledb.DB
	writer   *isledb.Writer
	compactor *isledb.Compactor
	cacheDir string
}

func openTenant(ctx context.Context, cfg StoreConfig, tenantID string) (*TenantPipeline, error) {
	store, err := openStore(ctx, cfg, tenantID)
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", tenantID, err)
	}

	db, err := isledb.OpenDB(ctx, store, isledb.DBOptions{})
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("open db %s: %w", tenantID, err)
	}

	wOpts := isledb.DefaultWriterOptions()
	wOpts.FlushInterval = 2 * time.Second
	writer, err := db.OpenWriter(ctx, wOpts)
	if err != nil {
		db.Close()
		store.Close()
		return nil, fmt.Errorf("open writer %s: %w", tenantID, err)
	}

	cOpts := isledb.DefaultCompactorOptions()
	cOpts.CheckInterval = 2 * time.Second
	compactor, err := db.OpenCompactor(ctx, cOpts)
	if err != nil {
		writer.Close()
		db.Close()
		store.Close()
		return nil, fmt.Errorf("open compactor %s: %w", tenantID, err)
	}
	compactor.Start()

	cache := cacheDir(cfg, tenantID)
	if err := os.MkdirAll(cache, 0755); err != nil {
		compactor.Close()
		writer.Close()
		db.Close()
		store.Close()
		return nil, err
	}

	return &TenantPipeline{
		ID:        tenantID,
		store:     store,
		db:        db,
		writer:    writer,
		compactor: compactor,
		cacheDir:  cache,
	}, nil
}

func (t *TenantPipeline) Put(ev model.Event) error {
	val, err := ev.ValueBytes()
	if err != nil {
		return err
	}
	return t.writer.Put(ev.KeyBytes(), val)
}

func (t *TenantPipeline) Flush(ctx context.Context) error {
	return t.writer.Flush(ctx)
}

func (t *TenantPipeline) Close(ctx context.Context) error {
	if err := t.writer.Flush(ctx); err != nil {
		return err
	}
	if err := t.writer.Close(); err != nil {
		return err
	}
	if err := t.compactor.RunCompaction(ctx); err != nil {
		return err
	}
	t.compactor.Stop()
	if err := t.compactor.Close(); err != nil {
		return err
	}
	if err := t.db.Close(); err != nil {
		return err
	}
	return t.store.Close()
}

func (t *TenantPipeline) ScanKeys(ctx context.Context) ([]string, error) {
	reader, err := isledb.OpenReader(ctx, t.store, isledb.ReaderOpenOptions{
		CacheDir: t.cacheDir,
	})
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	if err := reader.Refresh(ctx); err != nil {
		return nil, err
	}

	minKey := model.TenantPrefix(t.ID)
	maxKey := model.TenantUpperBound(t.ID)
	rows, err := reader.ScanLimit(ctx, minKey, maxKey, 0)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(rows))
	for _, kv := range rows {
		keys = append(keys, string(kv.Key))
	}
	return keys, nil
}

func (t *TenantPipeline) TailCatchUp(ctx context.Context) (int, error) {
	opts := isledb.DefaultTailingReaderOpenOptions()
	opts.ReaderOptions.CacheDir = t.cacheDir
	tr, err := isledb.OpenTailingReader(ctx, t.store, opts)
	if err != nil {
		return 0, err
	}
	defer tr.Close()

	if err := tr.Start(); err != nil {
		return 0, err
	}

	minKey := model.TenantPrefix(t.ID)
	maxKey := model.TenantUpperBound(t.ID)
	var count int
	result, err := tr.CatchUp(ctx, isledb.CatchUpOptions{
		MinKey: minKey,
		MaxKey: maxKey,
	}, func(kv isledb.KV) error {
		count++
		return nil
	})
	if err != nil {
		return count, err
	}
	return result.Count, nil
}

// Pipeline coordinates all tenant writers.
type Pipeline struct {
	tenants map[string]*TenantPipeline
	cfg     StoreConfig
}

func New(ctx context.Context, cfg StoreConfig, tenantIDs []string) (*Pipeline, error) {
	p := &Pipeline{
		tenants: make(map[string]*TenantPipeline),
		cfg:     cfg,
	}
	for _, id := range tenantIDs {
		tp, err := openTenant(ctx, cfg, id)
		if err != nil {
			p.Close(ctx)
			return nil, err
		}
		p.tenants[id] = tp
	}
	return p, nil
}

func (p *Pipeline) WriteEmissions(emissions []eventgen.Emission) (int, error) {
	puts := 0
	for _, em := range emissions {
		tp, ok := p.tenants[em.Event.TenantID]
		if !ok {
			return puts, fmt.Errorf("unknown tenant %q", em.Event.TenantID)
		}
		if err := tp.Put(em.Event); err != nil {
			return puts, err
		}
		puts++
	}
	return puts, nil
}

func (p *Pipeline) FlushAll(ctx context.Context) error {
	for _, tp := range p.tenants {
		if err := tp.Flush(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pipeline) CompactAll(ctx context.Context) error {
	for _, tp := range p.tenants {
		if err := tp.compactor.RunCompaction(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p *Pipeline) Close(ctx context.Context) error {
	var first error
	for _, tp := range p.tenants {
		if err := tp.Close(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (p *Pipeline) TenantIDs() []string {
	ids := make([]string, 0, len(p.tenants))
	for id := range p.tenants {
		ids = append(ids, id)
	}
	return ids
}

func (p *Pipeline) CountKeys(ctx context.Context) (int, error) {
	total := 0
	for _, id := range p.TenantIDs() {
		tp, err := p.tenant(id)
		if err != nil {
			return total, err
		}
		keys, err := tp.ScanKeys(ctx)
		if err != nil {
			return total, err
		}
		total += len(keys)
	}
	return total, nil
}

func (p *Pipeline) tenant(id string) (*TenantPipeline, error) {
	tp, ok := p.tenants[id]
	if !ok {
		return nil, fmt.Errorf("tenant %q not found", id)
	}
	return tp, nil
}
