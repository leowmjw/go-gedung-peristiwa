//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/eventgen"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestSimulationMinIO(t *testing.T) {
	if os.Getenv("MINIO_ENDPOINT") == "" {
		t.Skip("MINIO_ENDPOINT not set")
	}

	ctx := context.Background()
	genCfg := eventgen.DefaultConfig()
	genCfg.TotalUnique = 50
	genCfg.Duration = 5 * time.Second
	genCfg.DuplicateRate = 0.1
	genCfg.NoDelay = true

	emissions, err := eventgen.FastRun(ctx, genCfg)
	if err != nil {
		t.Fatal(err)
	}

	cfg := pipeline.StoreConfigFromEnv(pipeline.BackendMinIO, "e2e-test")
	cfg.CacheRoot = t.TempDir()

	p, err := pipeline.New(ctx, cfg, pipeline.AllTenantIDs)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	if _, err := p.WriteEmissions(emissions); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.CompactAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Verify(ctx, emissions); err != nil {
		t.Fatal(err)
	}
}
