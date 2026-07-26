package demo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	demopkg "github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	demoweb "github.com/leow/go-gedung-peristiwa/internal/web/demo"
)

func TestStreamRefreshOnPoll(t *testing.T) {
	positions := []gtfs.VehiclePosition{
		{Agency: "prasarana-rapid-bus-kl", VehicleID: "b1", Lat: 3.2, Lng: 101.7, Timestamp: time.Now()},
	}
	polls := make(chan struct{}, 1)
	src := &stubSource{
		positions: positions,
		polls:     polls,
		ingest: map[string][]demopkg.IngestRecord{
			"Klang Valley": {{Agency: "prasarana-rapid-bus-kl", BucketID: "klang-valley", BucketLabel: "Klang Valley", VehicleID: "b1"}},
		},
		polled: []string{"prasarana-rapid-bus-kl"},
	}

	srv := demoweb.NewServer(src, demopkg.NewSessionStore(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/vehicles/stream?region=klang-valley", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	polls <- struct{}{}
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	body := rec.Body.String()
	if strings.Count(body, "event: vehicles") < 2 {
		t.Fatalf("expected at least 2 vehicle snapshots, got:\n%s", body)
	}
	if !strings.Contains(body, "event: ingest") {
		t.Fatalf("missing ingest event:\n%s", body)
	}
}

func TestStreamIgnoresStaleRegionQuery(t *testing.T) {
	positions := []gtfs.VehiclePosition{
		{Agency: "prasarana-rapid-bus-kl", VehicleID: "kl1", Lat: 3.2, Lng: 101.7, Timestamp: time.Now()},
		{Agency: "mybas-johor", VehicleID: "j1", Lat: 1.5, Lng: 103.7, Timestamp: time.Now()},
	}
	ch := make(chan struct{})
	close(ch)
	srv := newTestServer(&stubSource{positions: positions, polls: ch})

	// Stale ?region=johor after server restart; session defaults to klang-valley.
	req := httptest.NewRequest(http.MethodGet, "/api/vehicles/stream?region=johor", nil)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "mybas-johor") {
		t.Fatalf("session region should win over stale query:\n%s", body)
	}
	if !strings.Contains(body, "prasarana-rapid-bus-kl") {
		t.Fatalf("missing kl vehicle:\n%s", body)
	}
}

func TestStreamFiltersByRegion(t *testing.T) {
	positions := []gtfs.VehiclePosition{
		{Agency: "prasarana-rapid-bus-kl", VehicleID: "kl1", Lat: 3.2, Lng: 101.7, Timestamp: time.Now()},
		{Agency: "mybas-johor", VehicleID: "j1", Lat: 1.5, Lng: 103.7, Timestamp: time.Now()},
	}
	ch := make(chan struct{})
	close(ch)
	srv := demoweb.NewServer(&stubSource{positions: positions, polls: ch}, demopkg.NewSessionStore(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/vehicles/stream?region=klang-valley", nil)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	body := rec.Body.String()
	if strings.Contains(body, "mybas-johor") {
		t.Fatalf("johor vehicle leaked into klang valley stream:\n%s", body)
	}
	if !strings.Contains(body, "prasarana-rapid-bus-kl") {
		t.Fatalf("missing kl vehicle:\n%s", body)
	}
}
