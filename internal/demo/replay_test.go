package demo_test

import (
	"context"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestRunReplayDedupeAndCarryForward(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{
		Backend:   pipeline.BackendMemory,
		CacheRoot: t.TempDir(),
	}
	feeds := []gtfs.Feed{
		{Agency: "ktmb"},
	}
	p, err := demo.NewPipeline(ctx, cfg, feeds)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	ts1 := time.Unix(1700000000, 0).UTC()
	ts2 := ts1.Add(time.Minute)
	if _, err := p.Write(ctx, []gtfs.VehiclePosition{
		{Agency: "ktmb", VehicleID: "t1", Lat: 3.1, Lng: 101.6, Timestamp: ts1, Route: "E1"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write(ctx, []gtfs.VehiclePosition{
		{Agency: "ktmb", VehicleID: "t1", Lat: 3.2, Lng: 101.7, Timestamp: ts2, Route: "E1"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}

	var frames [][]gtfs.VehiclePosition
	err = p.RunReplay(ctx, demo.ReplayOptions{
		RegionID: "national",
		Speed:    60,
	}, func(positions []gtfs.VehiclePosition, _ demo.ReplayProgress) error {
		frames = append(frames, append([]gtfs.VehiclePosition(nil), positions...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) < 2 {
		t.Fatalf("expected >=2 frames, got %d", len(frames))
	}
	last := frames[len(frames)-1]
	if len(last) != 1 || last[0].Lat != 3.2 {
		t.Fatalf("final frame: %+v", last)
	}
	if len(frames[0]) != 1 || frames[0][0].Lat != 3.1 {
		t.Fatalf("first frame should have earlier position: %+v", frames[0])
	}
}

func TestRunReplayCarryForwardAcrossAgencies(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{
		Backend:   pipeline.BackendMemory,
		CacheRoot: t.TempDir(),
	}
	feeds := []gtfs.Feed{
		{Agency: "mybas-kangar"},
		{Agency: "mybas-ipoh"},
	}
	p, err := demo.NewPipeline(ctx, cfg, feeds)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	ts := time.Unix(1700000000, 0).UTC()
	if _, err := p.Write(ctx, []gtfs.VehiclePosition{
		{Agency: "mybas-kangar", VehicleID: "k1", Lat: 6.4, Lng: 100.2, Timestamp: ts},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write(ctx, []gtfs.VehiclePosition{
		{Agency: "mybas-ipoh", VehicleID: "b9", Lat: 4.6, Lng: 101.1, Timestamp: ts},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}

	var lastCount int
	err = p.RunReplay(ctx, demo.ReplayOptions{RegionID: "northern", Speed: 60}, func(pos []gtfs.VehiclePosition, _ demo.ReplayProgress) error {
		lastCount = len(pos)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if lastCount != 2 {
		t.Fatalf("expected 2 vehicles after carry-forward, got %d", lastCount)
	}
}

// A resumed replay (From set, e.g. after a pause) must still carry forward
// vehicles whose last known position predates From, not just ones that
// mutate again after it — otherwise a tracked vehicle vanishes on resume.
func TestRunReplayFromCarriesForwardStaleVehicle(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{
		Backend:   pipeline.BackendMemory,
		CacheRoot: t.TempDir(),
	}
	feeds := []gtfs.Feed{
		{Agency: "mybas-kangar"},
		{Agency: "mybas-ipoh"},
	}
	p, err := demo.NewPipeline(ctx, cfg, feeds)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	ts1 := time.Unix(1700000000, 0).UTC()
	ts2 := ts1.Add(time.Minute)
	ts3 := ts1.Add(2 * time.Minute)

	// k1 only ever reports once, before the resume point.
	if _, err := p.Write(ctx, []gtfs.VehiclePosition{
		{Agency: "mybas-kangar", VehicleID: "k1", Lat: 6.4, Lng: 100.2, Timestamp: ts1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}
	// b9 keeps moving after the resume point.
	if _, err := p.Write(ctx, []gtfs.VehiclePosition{
		{Agency: "mybas-ipoh", VehicleID: "b9", Lat: 4.6, Lng: 101.1, Timestamp: ts2},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write(ctx, []gtfs.VehiclePosition{
		{Agency: "mybas-ipoh", VehicleID: "b9", Lat: 4.7, Lng: 101.2, Timestamp: ts3},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}

	var frames [][]gtfs.VehiclePosition
	err = p.RunReplay(ctx, demo.ReplayOptions{
		RegionID: "northern",
		Speed:    60,
		From:     ts2, // resume just as b9's first update lands
	}, func(pos []gtfs.VehiclePosition, _ demo.ReplayProgress) error {
		frames = append(frames, append([]gtfs.VehiclePosition(nil), pos...))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) == 0 {
		t.Fatal("expected at least one frame")
	}
	first := frames[0]
	hasK1 := false
	for _, v := range first {
		if v.Agency == "mybas-kangar" && v.VehicleID == "k1" {
			hasK1 = true
		}
	}
	if !hasK1 {
		t.Fatalf("expected stale vehicle k1 to be carried into resumed frame, got %+v", first)
	}
}
