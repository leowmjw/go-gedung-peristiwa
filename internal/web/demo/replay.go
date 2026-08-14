package demo

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	demopkg "github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

// ReplaySource catalogs manifest snapshots and runs historical playback.
type ReplaySource interface {
	CatalogForRegion(ctx context.Context, regionID string) (demopkg.RegionCatalog, error)
	RunReplay(ctx context.Context, opts demopkg.ReplayOptions, onFrame demopkg.ReplayFrameFunc) error
}

func (s *Server) handleReplayIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/replay" {
		http.NotFound(w, r)
		return
	}
	sid := sessionID(r)
	active := s.sessions.ActiveRegion(sid)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if err := replayTmpl.Execute(w, indexData{
		Regions:      gtfs.AllRegions(),
		ActiveRegion: active,
	}); err != nil {
		slog.Error("render replay", "err", err)
	}
}

func (s *Server) handleReplayCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.replay == nil {
		http.Error(w, "replay not configured", http.StatusServiceUnavailable)
		return
	}
	regionID := r.URL.Query().Get("region")
	if regionID == "" {
		regionID = s.sessions.Region(sessionID(r))
	}
	cat, err := s.replay.CatalogForRegion(r.Context(), regionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, cat)
}

func (s *Server) handleReplayStream(w http.ResponseWriter, r *http.Request) {
	if s.replay == nil {
		http.Error(w, "replay not configured", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()
	regionID := r.URL.Query().Get("region")
	if regionID == "" {
		regionID = s.sessions.Region(sessionID(r))
	}
	speed, _ := strconv.Atoi(r.URL.Query().Get("speed"))
	if speed <= 0 {
		speed = 1
	}
	opts := demopkg.ReplayOptions{
		RegionID:       regionID,
		Speed:          speed,
		FromSnapshotID: r.URL.Query().Get("from"),
		ToSnapshotID:   r.URL.Query().Get("to"),
	}

	initSSE(w)
	if err := writeSSE(w, "progress", map[string]any{"frame": 0, "total": 0, "status": "starting"}); err != nil {
		return
	}

	err := s.replay.RunReplay(ctx, opts, func(positions []gtfs.VehiclePosition, prog demopkg.ReplayProgress) error {
		if err := writeSSE(w, "vehicles", toViews(positions)); err != nil {
			return err
		}
		return writeSSE(w, "progress", map[string]any{
			"frame":  prog.Frame,
			"total":  prog.Total,
			"at":     formatReplayTime(prog.At),
			"status": "playing",
		})
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
			return
		}
		_ = writeSSE(w, "error", map[string]string{"message": err.Error()})
		return
	}
	_ = writeSSE(w, "done", map[string]string{"status": "complete"})
}

func formatReplayTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}
