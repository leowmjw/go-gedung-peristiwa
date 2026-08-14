package demo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	demopkg "github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
	demoweb "github.com/leow/go-gedung-peristiwa/internal/web/demo"
)

type replayStub struct {
	pipeline *demopkg.Pipeline
}

func (r *replayStub) CatalogForRegion(ctx context.Context, regionID string) (demopkg.RegionCatalog, error) {
	return r.pipeline.CatalogForRegion(ctx, regionID)
}

func (r *replayStub) RunReplay(ctx context.Context, opts demopkg.ReplayOptions, onFrame demopkg.ReplayFrameFunc) error {
	return r.pipeline.RunReplay(ctx, opts, onFrame)
}

func TestReplayCatalogEmptyRegion(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}
	p, err := demopkg.NewPipeline(ctx, cfg, []gtfs.Feed{{Agency: "mybas-johor"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	srv := demoweb.NewServer(&stubSource{}, &replayStub{pipeline: p}, demopkg.NewSessionStore(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/replay/catalog?region=johor", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cat demopkg.RegionCatalog
	if err := json.NewDecoder(rec.Body).Decode(&cat); err != nil {
		t.Fatal(err)
	}
	if !cat.Empty {
		t.Fatalf("expected empty catalog: %+v", cat)
	}
}

func TestReplayStreamFrames(t *testing.T) {
	ctx := context.Background()
	cfg := pipeline.StoreConfig{Backend: pipeline.BackendMemory, CacheRoot: t.TempDir()}
	p, err := demopkg.NewPipeline(ctx, cfg, []gtfs.Feed{{Agency: "ktmb"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)

	ts1 := time.Unix(1700000000, 0).UTC()
	ts2 := ts1.Add(time.Minute)
	if _, err := p.Write([]gtfs.VehiclePosition{
		{Agency: "ktmb", VehicleID: "t1", Lat: 3.1, Lng: 101.6, Timestamp: ts1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write([]gtfs.VehiclePosition{
		{Agency: "ktmb", VehicleID: "t1", Lat: 3.2, Lng: 101.7, Timestamp: ts2},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.FlushAll(ctx); err != nil {
		t.Fatal(err)
	}

	srv := demoweb.NewServer(&stubSource{}, &replayStub{pipeline: p}, demopkg.NewSessionStore(), nil)
	reqCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/replay/stream?region=national&speed=60", nil).WithContext(reqCtx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
	}

	body := rec.Body.String()
	if strings.Count(body, "event: vehicles") < 2 {
		t.Fatalf("expected >=2 vehicle frames, got:\n%s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatalf("missing done event:\n%s", body)
	}
}
