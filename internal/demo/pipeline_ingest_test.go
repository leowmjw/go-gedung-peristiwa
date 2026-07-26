package demo_test

import (
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestRecentIngestGroupedCap(t *testing.T) {
	ctx := t.Context()
	p, err := demo.NewPipeline(ctx, pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}, gtfs.AllFeeds())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	now := time.Now()
	for i := range 12 {
		_, err := p.Write([]gtfs.VehiclePosition{{
			Agency: "prasarana-rapid-bus-kl", VehicleID: "v" + string(rune('a'+i)),
			Lat: 3.1, Lng: 101.6, Timestamp: now.Add(time.Duration(i) * time.Second),
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	groups := p.RecentIngestGrouped()
	total := 0
	for _, recs := range groups {
		total += len(recs)
	}
	if total != 10 {
		t.Fatalf("total ingest records = %d want 10", total)
	}
}

func TestRecentIngestKTMBNational(t *testing.T) {
	ctx := t.Context()
	p, err := demo.NewPipeline(ctx, pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}, []gtfs.Feed{
		{Agency: "ktmb"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	_, err = p.Write([]gtfs.VehiclePosition{{
		Agency: "ktmb", VehicleID: "r1", Lat: 3.0, Lng: 101.0, Timestamp: time.Now(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	groups := p.RecentIngestGrouped()
	recs, ok := groups["National"]
	if !ok || len(recs) != 1 || recs[0].BucketID != "national" {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestRecentIngestForRegionFilter(t *testing.T) {
	ctx := t.Context()
	p, err := demo.NewPipeline(ctx, pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}, gtfs.AllFeeds())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	now := time.Now()
	_, err = p.Write([]gtfs.VehiclePosition{
		{Agency: "prasarana-rapid-bus-kl", VehicleID: "kl1", Lat: 3.1, Lng: 101.6, Timestamp: now},
		{Agency: "mybas-johor", VehicleID: "j1", Lat: 1.5, Lng: 103.7, Timestamp: now},
	})
	if err != nil {
		t.Fatal(err)
	}

	kl := p.RecentIngestForRegion("klang-valley")
	if len(kl) != 1 || kl[0].VehicleID != "kl1" {
		t.Fatalf("klang valley ingest = %+v", kl)
	}
	johor := p.RecentIngestForRegion("johor")
	if len(johor) != 1 || johor[0].VehicleID != "j1" {
		t.Fatalf("johor ingest = %+v", johor)
	}
}

func TestLatestPositionsForFilter(t *testing.T) {
	ctx := t.Context()
	p, err := demo.NewPipeline(ctx, pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}, gtfs.AllFeeds())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	now := time.Now()
	_, err = p.Write([]gtfs.VehiclePosition{
		{Agency: "prasarana-rapid-bus-kl", VehicleID: "a", Lat: 3.1, Lng: 101.6, Timestamp: now},
		{Agency: "mybas-johor", VehicleID: "b", Lat: 1.5, Lng: 103.7, Timestamp: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	kl := gtfs.AgencySet([]string{"prasarana-rapid-bus-kl"})
	pos := p.LatestPositionsFor(kl)
	if len(pos) != 1 || pos[0].Agency != "prasarana-rapid-bus-kl" {
		t.Fatalf("positions = %+v", pos)
	}
}
