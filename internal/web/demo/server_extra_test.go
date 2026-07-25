package demo_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	demoweb "github.com/leow/go-gedung-peristiwa/internal/web/demo"
)

func TestVehicleStreamPushesPositions(t *testing.T) {
	positions := []gtfs.VehiclePosition{{
		Agency: "ktmb", VehicleID: "t1", Lat: 3.1, Lng: 101.6, Route: "E1",
		Timestamp: time.Now(),
	}}
	ch := make(chan struct{})
	close(ch)

	srv := demoweb.NewServer(&stubSource{positions: positions, polls: ch}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/vehicles/stream", nil)
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
	if body == "" {
		t.Fatal("empty SSE body")
	}
}

func TestIndexNotFound(t *testing.T) {
	srv := demoweb.NewServer(&stubSource{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAgencyGroupViaIndex(t *testing.T) {
	srv := demoweb.NewServer(&stubSource{}, []gtfs.Feed{
		{Agency: "prasarana-rapid-bus-kl", Region: "KL"},
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
