package demo_test

import (
	"context"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

func TestNotifyPoll(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}
	p, err := demo.NewPipeline(ctx, cfg, testFeeds())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	sub := p.SubscribePolls(ctx)
	p.NotifyPoll()

	select {
	case <-sub:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for poll notification")
	}
}

func TestLatestPositions(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}
	p, err := demo.NewPipeline(ctx, cfg, testFeeds())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	ts := time.Now()
	_, err = p.Write([]gtfs.VehiclePosition{
		{Agency: "ktmb", VehicleID: "t1", Lat: 3.1, Lng: 101.6, Timestamp: ts},
	})
	if err != nil {
		t.Fatal(err)
	}

	latest := p.LatestPositions()
	if len(latest) != 1 || latest[0].VehicleID != "t1" {
		t.Fatalf("latest = %+v", latest)
	}
}
