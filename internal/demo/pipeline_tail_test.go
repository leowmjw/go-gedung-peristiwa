package demo_test

import (
	"context"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestPipelineMultipleChangeFeedWrites(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}
	p, err := demo.NewPipeline(ctx, cfg, testFeeds())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	ts := time.Unix(1700000000, 0).UTC()
	for i := range 3 {
		pos := gtfs.VehiclePosition{
			Agency: "ktmb", VehicleID: "t1",
			Lat: 3.0 + float64(i)*0.1, Lng: 101.6,
			Timestamp: ts.Add(time.Duration(i) * time.Minute),
		}
		if _, err := p.Write(ctx, []gtfs.VehiclePosition{pos}); err != nil {
			t.Fatal(err)
		}
		if err := p.FlushAll(ctx); err != nil {
			t.Fatal(err)
		}
	}

	cat, err := p.CatalogForRegion(ctx, "national")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Total < 3 {
		t.Fatalf("expected >=3 feed changes, got %d", cat.Total)
	}

	latest, err := p.ScanLatest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 1 {
		t.Fatalf("latest = %d", len(latest))
	}
}
