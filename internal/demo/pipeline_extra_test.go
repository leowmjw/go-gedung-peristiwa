package demo_test

import (
	"context"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestPipelineSetLastPollAndStats(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}
	p, err := demo.NewPipeline(ctx, cfg, testFeeds())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	now := time.Now().UTC().Truncate(time.Second)
	p.SetLastPoll(now)

	count, last, events := p.Stats()
	if count != 0 || events != 0 {
		t.Fatalf("stats before write: count=%d events=%d", count, events)
	}
	if !last.Equal(now) {
		t.Fatalf("last poll = %v want %v", last, now)
	}
}

func TestPipelineUnknownAgency(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}
	p, err := demo.NewPipeline(ctx, cfg, testFeeds())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	_, err = p.Write([]gtfs.VehiclePosition{{
		Agency: "unknown", VehicleID: "x", Lat: 3, Lng: 101, Timestamp: time.Now(),
	}})
	if err == nil {
		t.Fatal("expected error for unknown agency")
	}
}
