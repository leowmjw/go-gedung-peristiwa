package pipeline_test

import (
	"context"
	"math/rand"
	"testing"

	"github.com/leow/go-gedung-peristiwa/internal/eventgen"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestPipelineMemoryIntegration(t *testing.T) {
	ctx := context.Background()

	genCfg := eventgen.DefaultConfig()
	genCfg.TotalUnique = 80
	genCfg.DuplicateRate = 0.15
	genCfg.Rand = rand.New(rand.NewSource(99))

	emissions, err := eventgen.FastRun(ctx, genCfg)
	if err != nil {
		t.Fatal(err)
	}

	cfg := pipeline.StoreConfig{
		Backend:   pipeline.BackendMemory,
		CacheRoot: t.TempDir(),
	}

	p, err := pipeline.New(ctx, cfg, pipeline.AllTenantIDs)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	puts, err := p.WriteEmissions(emissions)
	if err != nil {
		t.Fatal(err)
	}
	if puts != len(emissions) {
		t.Fatalf("puts = %d, emissions = %d", puts, len(emissions))
	}

	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}

	pre, err := p.CountKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.CompactAll(ctx); err != nil {
		t.Fatal(err)
	}

	post, err := p.CountKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if post != pre {
		t.Fatalf("post-compact keys %d != pre %d", post, pre)
	}

	result, err := p.Verify(ctx, emissions)
	if err != nil {
		t.Fatal(err)
	}
	if result.UniqueKeysExpected != genCfg.TotalUnique {
		t.Fatalf("unique = %d", result.UniqueKeysExpected)
	}
}
