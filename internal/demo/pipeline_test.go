package demo_test

import (
	"context"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func testFeeds() []gtfs.Feed {
	return []gtfs.Feed{
		{Agency: "ktmb", Type: "rail", Region: "National", Group: "ktmb"},
		{Agency: "mybas-ipoh", Type: "bus", Region: "Perak", Group: "mybas"},
	}
}

func TestPipelineWriteScanTail(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{
		Backend:   pipeline.BackendMemory,
		CacheRoot: t.TempDir(),
	}

	p, err := demo.NewPipeline(ctx, cfg, testFeeds())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	ts1 := time.Unix(1700000000, 0).UTC()
	ts2 := ts1.Add(time.Minute)

	positions := []gtfs.VehiclePosition{
		{Agency: "ktmb", VehicleID: "t1", Lat: 3.1, Lng: 101.6, Timestamp: ts1, Route: "E1"},
		{Agency: "ktmb", VehicleID: "t1", Lat: 3.2, Lng: 101.7, Timestamp: ts2, Route: "E1"},
		{Agency: "mybas-ipoh", VehicleID: "b9", Lat: 4.6, Lng: 101.1, Timestamp: ts1},
	}

	tailCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates := p.TailUpdates(tailCtx)

	puts, err := p.Write(positions)
	if err != nil {
		t.Fatal(err)
	}
	if puts != 3 {
		t.Fatalf("puts = %d", puts)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}

	latest, err := p.ScanLatest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 2 {
		t.Fatalf("latest count = %d, want 2", len(latest))
	}

	byVehicle := map[string]gtfs.VehiclePosition{}
	for _, pos := range latest {
		byVehicle[pos.VehicleID] = pos
	}
	if byVehicle["t1"].Lat != 3.2 {
		t.Fatalf("expected latest lat 3.2, got %v", byVehicle["t1"].Lat)
	}

	count, _, events := p.Stats()
	if count != 2 || events != 3 {
		t.Fatalf("stats vehicles=%d events=%d", count, events)
	}

	// Tail should eventually see writes after flush.
	deadline := time.After(5 * time.Second)
	var tailCount int
	for tailCount < 1 {
		select {
		case _, ok := <-updates:
			if !ok {
				t.Fatal("tail channel closed early")
			}
			tailCount++
		case <-deadline:
			t.Fatal("timed out waiting for tail updates")
		}
	}
}
