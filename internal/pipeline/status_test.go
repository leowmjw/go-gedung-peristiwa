package pipeline_test

import (
	"context"
	"testing"

	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestProbeMemoryEmpty(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{
		Backend:   pipeline.BackendMemory,
		CacheRoot: t.TempDir(),
	}
	st := pipeline.Probe(ctx, cfg)
	if !st.OK {
		t.Fatalf("probe memory: %s", st.Error)
	}
	if st.TotalKeys != 0 {
		t.Fatalf("total keys = %d", st.TotalKeys)
	}
	for _, id := range pipeline.AllTenantIDs {
		if n, ok := st.KeysByTenant[id]; !ok || n != 0 {
			t.Fatalf("tenant %s keys = %d", id, n)
		}
	}
}
