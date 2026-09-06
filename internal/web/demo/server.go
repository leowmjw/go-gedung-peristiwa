package demo

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	demopkg "github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

// PipelineSource is the pipeline API used by the demo server.
type PipelineSource interface {
	LivePositionsFor(agencies map[string]struct{}, now time.Time) []gtfs.VehiclePosition
	SubscribePolls(ctx context.Context) <-chan struct{}
	StatsFor(agencies map[string]struct{}, now time.Time) (vehicleCount int, lastPoll time.Time, eventCount int64)
	RecentIngestForRegion(regionID string) []demopkg.IngestRecord
	LastPolledAgencies() []string
}

// RegionChangeFunc is called when a session switches region (region id for targeted poll).
type RegionChangeFunc func(regionID string)

// Server serves the transit map demo UI.
type Server struct {
	pipeline       PipelineSource
	replay         ReplaySource
	sessions       *demopkg.SessionStore
	onRegionChange RegionChangeFunc
}

// NewServer creates a demo HTTP server. replay may be nil to disable replay routes.
func NewServer(pipeline PipelineSource, replay ReplaySource, sessions *demopkg.SessionStore, onRegionChange RegionChangeFunc) *Server {
	return &Server{
		pipeline:       pipeline,
		replay:         replay,
		sessions:       sessions,
		onRegionChange: onRegionChange,
	}
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/regions", s.handleRegions)
	mux.HandleFunc("/api/region", s.handleRegion)
	mux.HandleFunc("/api/poll-interval", s.handlePollInterval)
	mux.HandleFunc("/api/vehicles", s.handleVehicles)
	mux.HandleFunc("/api/vehicles/stream", s.handleVehicleStream)
	mux.HandleFunc("/replay", s.handleReplayIndex)
	mux.HandleFunc("/api/replay/catalog", s.handleReplayCatalog)
	mux.HandleFunc("/api/replay/stream", s.handleReplayStream)
	return s.withSession(mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	sid := sessionID(r)
	active := s.sessions.ActiveRegion(sid)
	feeds, _ := s.sessions.ActiveFeeds(sid)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if err := indexTmpl.Execute(w, indexData{
		Regions:      gtfs.AllRegions(),
		ActiveRegion: active,
		Feeds:        feeds,
		PollSeconds:  s.sessions.PollSeconds(sid),
		PollOptions:  demopkg.AllowedPollSeconds,
	}); err != nil {
		slog.Error("render index", "err", err)
	}
}

func (s *Server) handleRegions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{
		"active":  s.sessions.Region(sessionID(r)),
		"regions": regionViews(gtfs.AllRegions(), s.sessions.Region(sessionID(r))),
	})
}

type setRegionRequest struct {
	ID string `json:"id"`
}

func (s *Server) handleRegion(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.postRegion(w, r)
	case http.MethodGet:
		s.getRegion(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getRegion(w http.ResponseWriter, r *http.Request) {
	region, feeds, err := s.activeRegionPayload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeRegionPayload(w, r, region, feeds)
}

func (s *Server) postRegion(w http.ResponseWriter, r *http.Request) {
	var req setRegionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := s.sessions.SetRegion(sessionID(r), req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.onRegionChange != nil {
		s.onRegionChange(req.ID)
	}
	region, feeds, err := s.activeRegionPayload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeRegionPayload(w, r, region, feeds)
}

func (s *Server) handlePollInterval(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.writePollInterval(w, r)
	case http.MethodPost:
		s.postPollInterval(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type setPollIntervalRequest struct {
	Seconds int `json:"seconds"`
}

func (s *Server) postPollInterval(w http.ResponseWriter, r *http.Request) {
	var req setPollIntervalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	sid := sessionID(r)
	if err := s.sessions.SetPollSeconds(sid, req.Seconds); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.onRegionChange != nil {
		s.onRegionChange(s.sessions.Region(sid))
	}
	s.writePollInterval(w, r)
}

func (s *Server) writePollInterval(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"seconds": s.sessions.PollSeconds(sessionID(r)),
		"options": demopkg.AllowedPollSeconds,
	})
}

func (s *Server) writeRegionPayload(w http.ResponseWriter, r *http.Request, region regionView, feeds []gtfs.Feed) {
	writeJSON(w, map[string]any{
		"region":      region,
		"feeds":       feedViews(feeds),
		"pollSeconds": s.sessions.PollSeconds(sessionID(r)),
	})
}

func (s *Server) activeRegionPayload(r *http.Request) (regionView, []gtfs.Feed, error) {
	sid := sessionID(r)
	rgn := s.sessions.ActiveRegion(sid)
	feeds, err := s.sessions.ActiveFeeds(sid)
	if err != nil {
		return regionView{}, nil, err
	}
	return regionView{
		ID:       rgn.ID,
		Label:    rgn.Label,
		Center:   rgn.Center,
		Zoom:     rgn.Zoom,
		Agencies: append([]string(nil), rgn.Agencies...),
	}, feeds, nil
}

func (s *Server) handleVehicles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sid := sessionID(r)
	agencies, err := s.sessions.ActiveAgencies(sid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	writeJSON(w, liveViews(s.pipeline.LivePositionsFor(agencies, now), now))
}

func (s *Server) handleVehicleStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sid := sessionID(r)
	s.sessions.AcquireLive(sid)
	defer s.sessions.ReleaseLive(sid)

	initSSE(w)
	agencies, err := s.sessions.ActiveAgencies(sid)
	if err != nil {
		slog.Error("active agencies", "err", err)
		return
	}

	pushSnapshot := func() error {
		now := time.Now().UTC()
		views := liveViews(s.pipeline.LivePositionsFor(agencies, now), now)
		if err := writeSSE(w, "vehicles", views); err != nil {
			return err
		}
		if err := s.pushStats(w, agencies, sid); err != nil {
			return err
		}
		return s.pushIngest(w, sid)
	}

	// Subscribe before the first push so we cannot miss NotifyPoll between them.
	polls := s.pipeline.SubscribePolls(ctx)

	if err := pushSnapshot(); err != nil {
		if !errors.Is(err, io.EOF) {
			slog.Error("push snapshot", "err", err)
		}
		return
	}

	heartbeat := time.NewTicker(demopkg.SessionHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			s.sessions.Touch(sid)
		case _, ok := <-polls:
			if !ok {
				return
			}
			s.sessions.Touch(sid)
			if err := pushSnapshot(); err != nil {
				if !errors.Is(err, io.EOF) {
					slog.Error("push snapshot", "err", err)
				}
				return
			}
		}
	}
}

type vehicleView struct {
	ID        string  `json:"id"`
	VehicleID string  `json:"vehicle_id"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Agency    string  `json:"agency"`
	Group     string  `json:"group"`
	Route     string  `json:"route"`
	Speed     float64 `json:"speed"`
	Bearing   float64 `json:"bearing"`
	Stale     bool    `json:"stale"`
}

type regionView struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Center   [2]float64 `json:"center"`
	Zoom     int        `json:"zoom"`
	Agencies []string   `json:"agencies"`
	Active   bool       `json:"active,omitempty"`
}

type feedView struct {
	Agency string `json:"agency"`
	Region string `json:"region"`
}

func regionViews(regions []gtfs.MapRegion, activeID string) []regionView {
	out := make([]regionView, 0, len(regions))
	for _, r := range regions {
		out = append(out, regionView{
			ID:       r.ID,
			Label:    r.Label,
			Center:   r.Center,
			Zoom:     r.Zoom,
			Agencies: append([]string(nil), r.Agencies...),
			Active:   r.ID == activeID,
		})
	}
	return out
}

func feedViews(feeds []gtfs.Feed) []feedView {
	out := make([]feedView, 0, len(feeds))
	for _, f := range feeds {
		out = append(out, feedView{Agency: f.Agency, Region: f.Region})
	}
	return out
}

func toView(pos gtfs.VehiclePosition) vehicleView {
	return vehicleView{
		ID:        pos.Agency + ":" + pos.VehicleID,
		VehicleID: pos.VehicleID,
		Lat:       pos.Lat,
		Lng:       pos.Lng,
		Agency:    pos.Agency,
		Group:     agencyGroup(pos.Agency),
		Route:     pos.Route,
		Speed:     pos.Speed,
		Bearing:   pos.Bearing,
	}
}

// liveViews marks vehicles the agency has not refreshed within
// LiveMapFreshWindow so the map can gray them out.
func liveViews(positions []gtfs.VehiclePosition, now time.Time) []vehicleView {
	out := make([]vehicleView, 0, len(positions))
	for _, pos := range positions {
		v := toView(pos)
		v.Stale = now.Sub(pos.Timestamp) > demopkg.LiveMapFreshWindow
		out = append(out, v)
	}
	return out
}

// replayViews renders historical frames. Staleness is a live-map concept, so
// replayed positions are never marked stale.
func replayViews(positions []gtfs.VehiclePosition) []vehicleView {
	out := make([]vehicleView, 0, len(positions))
	for _, pos := range positions {
		out = append(out, toView(pos))
	}
	return out
}

func (s *Server) pushStats(w http.ResponseWriter, agencies map[string]struct{}, sid string) error {
	count, lastPoll, events := s.pipeline.StatsFor(agencies, time.Now().UTC())
	last := "—"
	if !lastPoll.IsZero() {
		last = lastPoll.Format("15:04:05")
	}
	return writeSSE(w, "stats", map[string]any{
		"vehicleCount": count,
		"lastUpdate":   last,
		"eventCount":   events,
		"activeRegion": s.sessions.Region(sid),
	})
}

func (s *Server) pushIngest(w http.ResponseWriter, sid string) error {
	regionID := s.sessions.Region(sid)
	active := s.sessions.ActiveRegion(sid)
	agencies, err := s.sessions.ActiveAgencies(sid)
	if err != nil {
		return err
	}

	recs := s.pipeline.RecentIngestForRegion(regionID)
	rows := make([]map[string]any, 0, len(recs))
	for _, rec := range recs {
		at := "—"
		if !rec.At.IsZero() {
			at = rec.At.Format("15:04:05")
		}
		rows = append(rows, map[string]any{
			"agency":  rec.Agency,
			"vehicle": rec.VehicleID,
			"lat":     rec.Lat,
			"lng":     rec.Lng,
			"at":      at,
		})
	}

	var polled []string
	for _, agency := range s.pipeline.LastPolledAgencies() {
		if _, ok := agencies[agency]; ok {
			polled = append(polled, agency)
		}
	}

	return writeSSE(w, "ingest", map[string]any{
		"activeRegion":   regionID,
		"activeLabel":    active.Label,
		"polledAgencies": polled,
		"records":        rows,
	})
}

func agencyGroup(agency string) string {
	switch {
	case agency == "ktmb":
		return "ktmb"
	case strings.HasPrefix(agency, "prasarana"):
		return "prasarana"
	default:
		return "mybas"
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json", "err", err)
	}
}

type indexData struct {
	Regions      []gtfs.MapRegion
	ActiveRegion gtfs.MapRegion
	Feeds        []gtfs.Feed
	PollSeconds  int
	PollOptions  []int
}

var indexTmpl = template.Must(func() (*template.Template, error) {
	return template.New("index").Funcs(template.FuncMap{
		"mustJSON": mustJSON,
	}).Parse(indexHTML)
}())

// mustJSON marshals v as JSON for a <script> context. Returning template.JS
// prevents html/template from JS-escaping the already-quoted JSON (which
// produced "\"klang-valley\"" and broke first-load replay catalog fetches).
func mustJSON(v any) template.JS {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return template.JS(b)
}
