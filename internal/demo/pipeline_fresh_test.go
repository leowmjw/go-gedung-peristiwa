package demo_test

import (
	"context"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestLivePositionsForRespectsFreshnessWindows(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{
		Backend:   pipeline.BackendMemory,
		CacheRoot: t.TempDir(),
	}

	p, err := demo.NewPipeline(ctx, cfg, []gtfs.Feed{
		{Agency: "prasarana-rapid-bus-penang", Type: "bus", Region: "Penang", Group: "prasarana"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	now := time.Unix(1700000000, 0).UTC()
	positions := []gtfs.VehiclePosition{
		{Agency: "prasarana-rapid-bus-penang", VehicleID: "fresh", Lat: 5.4, Lng: 100.3, Timestamp: now.Add(-2 * time.Minute)},
		{Agency: "prasarana-rapid-bus-penang", VehicleID: "stale", Lat: 5.4, Lng: 100.3, Timestamp: now.Add(-10 * time.Minute)},
		{Agency: "prasarana-rapid-bus-penang", VehicleID: "ancient", Lat: 5.4, Lng: 100.3, Timestamp: now.Add(-45 * time.Minute)},
	}
	if _, err := p.Write(ctx, positions); err != nil {
		t.Fatal(err)
	}

	agencies := gtfs.AgencySet([]string{"prasarana-rapid-bus-penang"})

	live := p.LivePositionsFor(agencies, now)
	if len(live) != 2 {
		t.Fatalf("LivePositionsFor count = %d, want 2 (fresh + stale)", len(live))
	}
	for _, pos := range live {
		if pos.VehicleID == "ancient" {
			t.Fatal("ancient vehicle should be hidden from live map")
		}
	}

	count, _, _ := p.StatsFor(agencies, now)
	if count != 2 {
		t.Fatalf("StatsFor visible count = %d, want 2", count)
	}
}
