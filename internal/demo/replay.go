package demo

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

const replayFrameInterval = time.Second

// ReplayOptions configures historical playback over manifest snapshot timelines.
type ReplayOptions struct {
	RegionID       string
	Speed          int // 1, 10, or 60
	FromSnapshotID string
	ToSnapshotID   string
}

// ReplayProgress is sent with each replay frame.
type ReplayProgress struct {
	Frame int       `json:"frame"`
	Total int       `json:"total"`
	At    time.Time `json:"at"`
}

// ReplayFrameFunc receives merged vehicle positions for one timeline frame.
type ReplayFrameFunc func(positions []gtfs.VehiclePosition, progress ReplayProgress) error

type replayFrameEvent struct {
	agency     string
	snapshotID string
	at         time.Time
	watermark  int64
}

// RunReplay walks the merged per-agency manifest snapshot timeline for a region.
// IsleDB OpenReader reads CURRENT topology; each frame approximates state after snapshot S
// by scanning keys with timestamp_ns <= watermark(S).
func (p *Pipeline) RunReplay(ctx context.Context, opts ReplayOptions, onFrame ReplayFrameFunc) error {
	if opts.Speed <= 0 {
		opts.Speed = 1
	}
	events, err := p.buildReplayTimeline(ctx, opts)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}

	region, err := gtfs.RegionByID(opts.RegionID)
	if err != nil {
		return err
	}
	agencySet := make(map[string]struct{}, len(region.Agencies))
	for _, a := range region.Agencies {
		agencySet[a] = struct{}{}
	}

	merged := make(map[string]gtfs.VehiclePosition)
	delay := replayFrameInterval / time.Duration(opts.Speed)

	for i, ev := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		aw, ok := p.agencies[ev.agency]
		if !ok {
			continue
		}
		positions, err := aw.scanThrough(ctx, ev.watermark)
		if err != nil {
			return fmt.Errorf("scan %s: %w", ev.agency, err)
		}
		for _, pos := range positions {
			key := pos.Agency + ":" + pos.VehicleID
			merged[key] = pos
		}
		out := make([]gtfs.VehiclePosition, 0, len(merged))
		for _, pos := range merged {
			if _, ok := agencySet[pos.Agency]; ok {
				out = append(out, pos)
			}
		}
		prog := ReplayProgress{
			Frame: i + 1,
			Total: len(events),
			At:    ev.at,
		}
		if err := onFrame(out, prog); err != nil {
			return err
		}
		if i+1 < len(events) {
			if err := sleepCtx(ctx, delay); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Pipeline) buildReplayTimeline(ctx context.Context, opts ReplayOptions) ([]replayFrameEvent, error) {
	cat, err := p.CatalogForRegion(ctx, opts.RegionID)
	if err != nil {
		return nil, err
	}
	var events []replayFrameEvent
	for _, ac := range cat.Agencies {
		for _, snap := range ac.Snapshots {
			if opts.FromSnapshotID != "" && snap.ID < opts.FromSnapshotID {
				continue
			}
			if opts.ToSnapshotID != "" && snap.ID > opts.ToSnapshotID {
				continue
			}
			at := snap.At
			wm := snap.WatermarkNS
			if wm <= 0 {
				wm = at.UnixNano()
			}
			if wm <= 0 {
				wm = time.Now().UnixNano()
			}
			events = append(events, replayFrameEvent{
				agency:     snap.Agency,
				snapshotID: snap.ID,
				at:         at,
				watermark:  wm,
			})
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if !events[i].at.Equal(events[j].at) {
			return events[i].at.Before(events[j].at)
		}
		if events[i].agency != events[j].agency {
			return events[i].agency < events[j].agency
		}
		return events[i].snapshotID < events[j].snapshotID
	})
	return events, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
