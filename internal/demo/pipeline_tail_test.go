package demo_test

import (
	"context"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestPipelineMultipleTailUpdates(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}
	p, err := demo.NewPipeline(ctx, cfg, testFeeds())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	tailCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates := p.TailUpdates(tailCtx)

	ts := time.Unix(1700000000, 0).UTC()
	for i := range 3 {
		pos := gtfs.VehiclePosition{
			Agency: "ktmb", VehicleID: "t1",
			Lat: 3.0 + float64(i)*0.1, Lng: 101.6,
			Timestamp: ts.Add(time.Duration(i) * time.Minute),
		}
		if _, err := p.Write([]gtfs.VehiclePosition{pos}); err != nil {
			t.Fatal(err)
		}
		if err := p.FlushAll(ctx); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.After(8 * time.Second)
	received := 0
	for received < 1 {
		select {
		case _, ok := <-updates:
			if !ok {
				t.Fatal("channel closed")
			}
			received++
		case <-deadline:
			t.Fatalf("received %d tail updates", received)
		}
	}

	latest, err := p.ScanLatest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 1 {
		t.Fatalf("latest = %d", len(latest))
	}
}
