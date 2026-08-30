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

func (s *stubSource) LivePositionsFor(agencies map[string]struct{}, now time.Time) []gtfs.VehiclePosition {
	var out []gtfs.VehiclePosition
	for _, p := range s.positions {
		if _, ok := agencies[p.Agency]; ok && now.Sub(p.Timestamp) <= demopkg.LiveMapVisibleWindow {
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

func (s *stubSource) StatsFor(agencies map[string]struct{}, now time.Time) (int, time.Time, int64) {
	return len(s.LivePositionsFor(agencies, now)), s.lastPoll, s.events
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
	return demoweb.NewServer(stub, nil, demopkg.NewSessionStore(), nil)
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

func TestGetVehiclesStaleFlag(t *testing.T) {
	now := time.Now().UTC()
	positions := []gtfs.VehiclePosition{
		{Agency: "prasarana-rapid-bus-kl", VehicleID: "fresh", Lat: 3.2, Lng: 101.7, Timestamp: now.Add(-2 * time.Minute)},
		{Agency: "prasarana-rapid-bus-kl", VehicleID: "stale", Lat: 3.2, Lng: 101.7, Timestamp: now.Add(-10 * time.Minute)},
		{Agency: "prasarana-rapid-bus-kl", VehicleID: "ancient", Lat: 3.2, Lng: 101.7, Timestamp: now.Add(-45 * time.Minute)},
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
	if len(views) != 2 {
		t.Fatalf("expected 2 visible vehicles (fresh + stale), got %d", len(views))
	}
	byID := map[string]bool{}
	for _, v := range views {
		byID[v["id"].(string)] = v["stale"].(bool)
	}
	if stale, ok := byID["prasarana-rapid-bus-kl:fresh"]; !ok || stale {
		t.Fatalf("fresh vehicle should be present and not stale: %+v", byID)
	}
	if stale, ok := byID["prasarana-rapid-bus-kl:stale"]; !ok || !stale {
		t.Fatalf("stale vehicle should be present and marked stale: %+v", byID)
	}
	if _, ok := byID["prasarana-rapid-bus-kl:ancient"]; ok {
		t.Fatal("ancient vehicle should be hidden from live map")
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
	if !strings.Contains(body, `id="poll-interval"`) {
		t.Fatal("missing poll interval control")
	}
	if !strings.Contains(body, `data-poll="10"`) || !strings.Contains(body, "seg-btn active") {
		t.Fatal("missing default 10s poll selection")
	}
	if !strings.Contains(body, `data-poll="20"`) || !strings.Contains(body, `data-poll="30"`) {
		t.Fatal("missing 20s/30s poll options")
	}
	if strings.Contains(body, `data-poll="5"`) {
		t.Fatal("5s poll option should be removed")
	}
	if strings.Contains(body, `"\"klang-valley\""`) {
		t.Fatal("mustJSON double-escaped activeRegion")
	}
	if !strings.Contains(body, `const activeRegion = "klang-valley";`) {
		t.Fatal("activeRegion should be a JSON string, not a quoted-and-escaped string")
	}
	if !strings.Contains(body, `let pollSeconds = 10;`) {
		t.Fatal("pollSeconds should be a JSON number")
	}
	if strings.Contains(body, `new Set("[`) {
		t.Fatal("currentAgencies should be a JSON array, not a quoted string")
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

func TestPollIntervalDefaultAndSet(t *testing.T) {
	srv := newTestServer(&stubSource{})

	recGet := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recGet, httptest.NewRequest(http.MethodGet, "/api/poll-interval", nil))
	if recGet.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", recGet.Code, recGet.Body.String())
	}
	cookie := sessionCookie(recGet)
	var got map[string]any
	if err := json.NewDecoder(recGet.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["seconds"] != float64(10) {
		t.Fatalf("default seconds = %v", got["seconds"])
	}

	body := bytes.NewBufferString(`{"seconds":20}`)
	req := withSessionCookie(httptest.NewRequest(http.MethodPost, "/api/poll-interval", body), cookie)
	recPost := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recPost, req)
	if recPost.Code != http.StatusOK {
		t.Fatalf("post status = %d body=%s", recPost.Code, recPost.Body.String())
	}
	var posted map[string]any
	if err := json.NewDecoder(recPost.Body).Decode(&posted); err != nil {
		t.Fatal(err)
	}
	if posted["seconds"] != float64(20) {
		t.Fatalf("posted seconds = %v", posted["seconds"])
	}

	reqGet := withSessionCookie(httptest.NewRequest(http.MethodGet, "/api/region", nil), cookie)
	recRegion := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recRegion, reqGet)
	var regionResp map[string]any
	if err := json.NewDecoder(recRegion.Body).Decode(&regionResp); err != nil {
		t.Fatal(err)
	}
	if regionResp["pollSeconds"] != float64(20) {
		t.Fatalf("region pollSeconds = %v", regionResp["pollSeconds"])
	}
}

func TestPollIntervalRejectsInvalid(t *testing.T) {
	srv := newTestServer(&stubSource{})
	body := bytes.NewBufferString(`{"seconds":7}`)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/poll-interval", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPollIntervalRejectsBadJSONAndMethod(t *testing.T) {
	srv := newTestServer(&stubSource{})

	recJSON := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recJSON, httptest.NewRequest(http.MethodPost, "/api/poll-interval", bytes.NewBufferString(`{`)))
	if recJSON.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d", recJSON.Code)
	}

	recMethod := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recMethod, httptest.NewRequest(http.MethodPut, "/api/poll-interval", nil))
	if recMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status = %d", recMethod.Code)
	}
}

func TestPollIntervalIndependentSessions(t *testing.T) {
	srv := newTestServer(&stubSource{})

	recA := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recA, httptest.NewRequest(http.MethodGet, "/api/poll-interval", nil))
	cookieA := sessionCookie(recA)

	recB := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recB, httptest.NewRequest(http.MethodGet, "/api/poll-interval", nil))
	cookieB := sessionCookie(recB)

	body := bytes.NewBufferString(`{"seconds":30}`)
	reqA := withSessionCookie(httptest.NewRequest(http.MethodPost, "/api/poll-interval", body), cookieA)
	recPost := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recPost, reqA)
	if recPost.Code != http.StatusOK {
		t.Fatalf("post A status = %d", recPost.Code)
	}

	reqB := withSessionCookie(httptest.NewRequest(http.MethodGet, "/api/poll-interval", nil), cookieB)
	recB2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recB2, reqB)
	var respB map[string]any
	if err := json.NewDecoder(recB2.Body).Decode(&respB); err != nil {
		t.Fatal(err)
	}
	if respB["seconds"] != float64(10) {
		t.Fatalf("browser B seconds = %v", respB["seconds"])
	}
}

func TestPollIntervalTriggersRefresh(t *testing.T) {
	var got string
	srv := demoweb.NewServer(&stubSource{}, nil, demopkg.NewSessionStore(), func(regionID string) {
		got = regionID
	})
	body := bytes.NewBufferString(`{"seconds":20}`)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/poll-interval", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got != "klang-valley" {
		t.Fatalf("onRegionChange = %q", got)
	}
}
