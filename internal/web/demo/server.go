package demo

import (
	"context"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

// PositionSource provides vehicle positions and stats for the demo UI.
type PositionSource interface {
	LatestPositions() []gtfs.VehiclePosition
	SubscribePolls(ctx context.Context) <-chan struct{}
	Stats() (vehicleCount int, lastPoll time.Time, eventCount int64)
}

// Server serves the transit map demo UI.
type Server struct {
	pipeline PositionSource
	feeds    []gtfs.Feed
}

// NewServer creates a demo HTTP server.
func NewServer(pipeline PositionSource, feeds []gtfs.Feed) *Server {
	return &Server{pipeline: pipeline, feeds: feeds}
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/vehicles/stream", s.handleVehicleStream)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTmpl.Execute(w, indexData{Feeds: s.feeds}); err != nil {
		slog.Error("render index", "err", err)
	}
}

func (s *Server) handleVehicleStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	initSSE(w)

	pushSnapshot := func() error {
		views := toViews(s.pipeline.LatestPositions())
		if err := writeSSE(w, "vehicles", views); err != nil {
			return err
		}
		return s.pushStats(w)
	}

	if err := pushSnapshot(); err != nil {
		slog.Error("push snapshot", "err", err)
		return
	}

	polls := s.pipeline.SubscribePolls(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-polls:
			if !ok {
				return
			}
			if err := pushSnapshot(); err != nil {
				slog.Error("push snapshot", "err", err)
				return
			}
		}
	}
}

type vehicleView struct {
	ID      string  `json:"id"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Agency  string  `json:"agency"`
	Group   string  `json:"group"`
	Route   string  `json:"route"`
	Speed   float64 `json:"speed"`
	Bearing float64 `json:"bearing"`
}

func toView(pos gtfs.VehiclePosition) vehicleView {
	return vehicleView{
		ID:      pos.Agency + ":" + pos.VehicleID,
		Lat:     pos.Lat,
		Lng:     pos.Lng,
		Agency:  pos.Agency,
		Group:   agencyGroup(pos.Agency),
		Route:   pos.Route,
		Speed:   pos.Speed,
		Bearing: pos.Bearing,
	}
}

func toViews(positions []gtfs.VehiclePosition) []vehicleView {
	out := make([]vehicleView, 0, len(positions))
	for _, pos := range positions {
		out = append(out, toView(pos))
	}
	return out
}

func (s *Server) pushStats(w http.ResponseWriter) error {
	count, lastPoll, events := s.pipeline.Stats()
	last := "—"
	if !lastPoll.IsZero() {
		last = lastPoll.Format("15:04:05")
	}
	return writeSSE(w, "stats", map[string]any{
		"vehicleCount": count,
		"lastUpdate":   last,
		"eventCount":   events,
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

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

type indexData struct {
	Feeds []gtfs.Feed
}

var indexTmpl = template.Must(template.New("index").Parse(indexHTML))
