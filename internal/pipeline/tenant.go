package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/eventgen"
	"github.com/leow/go-gedung-peristiwa/internal/model"
)

const tenantFlushInterval = 2 * time.Second

// TenantPipeline is one IsleDB prefix (single writer).
type TenantPipeline struct {
	*PrefixDB
}

func openTenant(ctx context.Context, cfg StoreConfig, tenantID string) (*TenantPipeline, error) {
	pdb, err := OpenPrefixDB(ctx, PrefixOpenConfig{
		Store:      cfg,
		PrefixID:   tenantID,
		FlushEvery: tenantFlushInterval,
		Retention:  cfg.Backend != BackendMemory,
		RunMaint:   cfg.Backend != BackendMemory,
		MaintCtx:   ctx,
	})
	if err != nil {
		return nil, err
	}
	return &TenantPipeline{PrefixDB: pdb}, nil
}

func (t *TenantPipeline) Put(ctx context.Context, ev model.Event) error {
	val, err := ev.ValueBytes()
	if err != nil {
		return err
	}
	return t.Writer.Put(ctx, ev.KeyBytes(), val)
}

func (t *TenantPipeline) Flush(ctx context.Context) error {
	if err := t.Writer.Flush(ctx); err != nil {
		return err
	}
	return RefreshReader(ctx, t.Reader)
}

func (t *TenantPipeline) ScanKeys(ctx context.Context) ([]string, error) {
	minKey := model.TenantPrefix(t.ID)
	maxKey := model.TenantUpperBound(t.ID)
	return ScanKeysIter(ctx, t.Reader, minKey, maxKey)
}

func (t *TenantPipeline) ChangeFeedCount(ctx context.Context) (int, error) {
	return CountChangeFeed(ctx, t.DB)
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

func (p *Pipeline) WriteEmissions(ctx context.Context, emissions []eventgen.Emission) (int, error) {
	puts := 0
	for _, em := range emissions {
		tp, ok := p.tenants[em.Event.TenantID]
		if !ok {
			return puts, fmt.Errorf("unknown tenant %q", em.Event.TenantID)
		}
		if err := tp.Put(ctx, em.Event); err != nil {
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
		if err := tp.RunMaintenanceOnce(ctx); err != nil {
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
