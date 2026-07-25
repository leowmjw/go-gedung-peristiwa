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

type stubSource struct {
	positions []gtfs.VehiclePosition
	polls     chan struct{}
}

func (s *stubSource) LatestPositions() []gtfs.VehiclePosition {
	return s.positions
}

func (s *stubSource) SubscribePolls(ctx context.Context) <-chan struct{} {
	if s.polls != nil {
		return s.polls
	}
	ch := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch
}

func (s *stubSource) Stats() (int, time.Time, int64) {
	return len(s.positions), time.Now(), int64(len(s.positions))
}

func TestIndexPage(t *testing.T) {
	srv := demoweb.NewServer(&stubSource{}, gtfs.KLFeeds())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "KL Transit Live") {
		t.Fatalf("missing title")
	}
	if !strings.Contains(body, "prasarana-rapid-bus-kl") {
		t.Fatalf("missing agency")
	}
}

func TestVehicleStreamHeaders(t *testing.T) {
	ch := make(chan struct{})
	close(ch)
	srv := demoweb.NewServer(&stubSource{polls: ch}, nil)
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
		t.Fatal("stream handler did not return")
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
}
