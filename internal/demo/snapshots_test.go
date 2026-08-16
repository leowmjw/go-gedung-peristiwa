package demo_test

import (
	"context"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestCatalogAfterFlush(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{
		Backend:   pipeline.BackendMemory,
		CacheRoot: t.TempDir(),
	}
	p, err := demo.NewPipeline(ctx, cfg, []gtfs.Feed{
		{Agency: "ktmb"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	ts := time.Unix(1700000000, 0).UTC()
	if _, err := p.Write(ctx, []gtfs.VehiclePosition{
		{Agency: "ktmb", VehicleID: "t1", Lat: 3.1, Lng: 101.6, Timestamp: ts},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}

	cat, err := p.CatalogForRegion(ctx, "national")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Total == 0 {
		t.Fatal("expected change-feed history after flush")
	}
}

func TestCatalogForRegionEmpty(t *testing.T) {
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

	cat, err := p.CatalogForRegion(ctx, "johor")
	if err != nil {
		t.Fatal(err)
	}
	if !cat.Empty {
		t.Fatalf("expected empty catalog, total=%d", cat.Total)
	}
	if cat.Message == "" {
		t.Fatal("expected empty message")
	}
	if len(cat.Agencies) != 1 || cat.Agencies[0].Agency != "mybas-johor" {
		t.Fatalf("unexpected agencies: %+v", cat.Agencies)
	}
}

func TestCatalogForRegionSparse(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{
		Backend:   pipeline.BackendMemory,
		CacheRoot: t.TempDir(),
	}
	feeds := []gtfs.Feed{
		{Agency: "prasarana-rapid-bus-kl"},
		{Agency: "prasarana-rapid-bus-mrtfeeder"},
	}
	p, err := demo.NewPipeline(ctx, cfg, feeds)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	ts := time.Unix(1700000000, 0).UTC()
	if _, err := p.Write(ctx, []gtfs.VehiclePosition{
		{Agency: "prasarana-rapid-bus-kl", VehicleID: "b1", Lat: 3.2, Lng: 101.7, Timestamp: ts},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}

	cat, err := p.CatalogForRegion(ctx, "klang-valley")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Empty {
		t.Fatal("expected non-empty region")
	}
	var withData, without int
	for _, ac := range cat.Agencies {
		if ac.Count > 0 {
			withData++
		} else {
			without++
		}
	}
	if withData != 1 || without != 1 {
		t.Fatalf("sparse agencies: with=%d without=%d", withData, without)
	}
}
