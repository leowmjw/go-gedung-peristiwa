package demo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	demoweb "github.com/leow/go-gedung-peristiwa/internal/web/demo"
)

func TestStreamRefreshOnPoll(t *testing.T) {
	positions := []gtfs.VehiclePosition{
		{Agency: "prasarana-rapid-bus-kl", VehicleID: "b1", Lat: 3.2, Lng: 101.7, Timestamp: time.Now()},
	}
	polls := make(chan struct{}, 1)
	src := &stubSource{positions: positions, polls: polls}

	srv := demoweb.NewServer(src, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/vehicles/stream", nil).WithContext(ctx)
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
}
