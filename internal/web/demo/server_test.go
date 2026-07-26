package demo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	demopkg "github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	demoweb "github.com/leow/go-gedung-peristiwa/internal/web/demo"
)

type stubSource struct {
	positions []gtfs.VehiclePosition
	polls     chan struct{}
	ingest    map[string][]demopkg.IngestRecord
	polled    []string
	lastPoll  time.Time
	events    int64
}

func (s *stubSource) LatestPositionsFor(agencies map[string]struct{}) []gtfs.VehiclePosition {
	var out []gtfs.VehiclePosition
	for _, p := range s.positions {
		if _, ok := agencies[p.Agency]; ok {
			out = append(out, p)
		}
	}
	return out
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

func (s *stubSource) StatsFor(agencies map[string]struct{}) (int, time.Time, int64) {
	return len(s.LatestPositionsFor(agencies)), s.lastPoll, s.events
}

func (s *stubSource) RecentIngestForRegion(regionID string) []demopkg.IngestRecord {
	if s.ingest == nil {
		return nil
	}
	var out []demopkg.IngestRecord
	for _, recs := range s.ingest {
		for _, rec := range recs {
			if rec.BucketID == regionID {
				out = append(out, rec)
			}
		}
	}
	return out
}

func (s *stubSource) LastPolledAgencies() []string {
	return s.polled
}

func newTestServer(stub *stubSource) *demoweb.Server {
	return demoweb.NewServer(stub, demopkg.NewSessionStore(), nil)
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == demopkg.SessionCookieName {
			return c
		}
	}
	return nil
}

func withSessionCookie(req *http.Request, cookie *http.Cookie) *http.Request {
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func TestSessionRegionsIndependent(t *testing.T) {
	srv := newTestServer(&stubSource{})

	recA := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recA, httptest.NewRequest(http.MethodGet, "/api/region", nil))
	cookieA := sessionCookie(recA)
	if cookieA == nil {
		t.Fatal("missing session cookie for browser A")
	}

	body := bytes.NewBufferString(`{"id":"johor"}`)
	reqA := withSessionCookie(httptest.NewRequest(http.MethodPost, "/api/region", body), cookieA)
	recPost := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recPost, reqA)
	if recPost.Code != http.StatusOK {
		t.Fatalf("post A status = %d body=%s", recPost.Code, recPost.Body.String())
	}

	recB := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recB, httptest.NewRequest(http.MethodGet, "/api/region", nil))
	cookieB := sessionCookie(recB)
	if cookieB == nil || cookieB.Value == cookieA.Value {
		t.Fatal("browser B should have its own session")
	}

	reqB := withSessionCookie(httptest.NewRequest(http.MethodGet, "/api/region", nil), cookieB)
	recB2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recB2, reqB)
	var respB map[string]any
	if err := json.NewDecoder(recB2.Body).Decode(&respB); err != nil {
		t.Fatal(err)
	}
	regionB, _ := respB["region"].(map[string]any)
	if regionB["id"] != "klang-valley" {
		t.Fatalf("browser B region = %v", regionB["id"])
	}

	reqA2 := withSessionCookie(httptest.NewRequest(http.MethodGet, "/api/region", nil), cookieA)
	recA2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recA2, reqA2)
	var respA map[string]any
	if err := json.NewDecoder(recA2.Body).Decode(&respA); err != nil {
		t.Fatal(err)
	}
	regionA, _ := respA["region"].(map[string]any)
	if regionA["id"] != "johor" {
		t.Fatalf("browser A region = %v", regionA["id"])
	}
}

func TestGetVehicles(t *testing.T) {
	positions := []gtfs.VehiclePosition{
		{Agency: "prasarana-rapid-bus-kl", VehicleID: "kl1", Lat: 3.2, Lng: 101.7, Timestamp: time.Now()},
		{Agency: "mybas-johor", VehicleID: "j1", Lat: 1.5, Lng: 103.7, Timestamp: time.Now()},
	}
	srv := newTestServer(&stubSource{positions: positions})
	req := httptest.NewRequest(http.MethodGet, "/api/vehicles?region=klang-valley", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var views []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&views); err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 {
		t.Fatalf("expected 1 vehicle, got %d", len(views))
	}
	if views[0]["agency"] != "prasarana-rapid-bus-kl" {
		t.Fatalf("vehicle = %+v", views[0])
	}
}

func TestIndexPage(t *testing.T) {
	srv := newTestServer(&stubSource{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Malaysia Transit Live") {
		t.Fatalf("missing title")
	}
	if !strings.Contains(body, "klang-valley") {
		t.Fatalf("missing region switcher")
	}
	if !strings.Contains(body, "debug-ingest") {
		t.Fatalf("missing debug toggle")
	}
}

func TestVehicleStreamHeaders(t *testing.T) {
	ch := make(chan struct{})
	close(ch)
	srv := newTestServer(&stubSource{polls: ch})
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
		t.Fatal("stream handler did not return")
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestPostRegion(t *testing.T) {
	srv := newTestServer(&stubSource{})
	body := bytes.NewBufferString(`{"id":"johor"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/region", body)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	region, ok := resp["region"].(map[string]any)
	if !ok || region["id"] != "johor" {
		t.Fatalf("region = %+v", resp["region"])
	}
}

func TestGetRegions(t *testing.T) {
	srv := newTestServer(&stubSource{})
	req := httptest.NewRequest(http.MethodGet, "/api/regions", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
