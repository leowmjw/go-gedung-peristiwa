package demo_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

func TestVehicleStreamPushesPositions(t *testing.T) {
	positions := []gtfs.VehiclePosition{{
		Agency: "ktmb", VehicleID: "t1", Lat: 3.1, Lng: 101.6, Route: "E1",
		Timestamp: time.Now(),
	}}
	ch := make(chan struct{})
	close(ch)

	srv := newTestServer(&stubSource{positions: positions, polls: ch})
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
	if body == "" {
		t.Fatal("empty SSE body")
	}
}

func TestIndexNotFound(t *testing.T) {
	srv := newTestServer(&stubSource{})
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestIndexRendersActiveRegionTenants(t *testing.T) {
	srv := newTestServer(&stubSource{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "prasarana-rapid-bus-kl") {
		t.Fatal("missing klang valley tenant")
	}
}
