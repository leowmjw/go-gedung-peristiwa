package demo

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ankur-anand/isledb"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

const replayFrameInterval = time.Second

// ReplayOptions configures historical playback over the change feed.
type ReplayOptions struct {
	RegionID string
	Speed    int // 1, 10, or 60
	From     time.Time
	To       time.Time
}

// ReplayProgress is sent with each replay frame.
type ReplayProgress struct {
	Frame int       `json:"frame"`
	Total int       `json:"total"`
	At    time.Time `json:"at"`
}

// ReplayFrameFunc receives merged vehicle positions for one timeline frame.
type ReplayFrameFunc func(positions []gtfs.VehiclePosition, progress ReplayProgress) error

type replayFrame struct {
	at time.Time
}

type replayMutation struct {
	at  time.Time
	pos gtfs.VehiclePosition
}

// RunReplay walks the merged per-agency change feed for a region.
func (p *Pipeline) RunReplay(ctx context.Context, opts ReplayOptions, onFrame ReplayFrameFunc) error {
	if opts.Speed <= 0 {
		opts.Speed = 1
	}
	frames, err := p.buildReplayFrames(ctx, opts)
	if err != nil {
		return err
	}
	if len(frames) == 0 {
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

	for i, frame := range frames {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, mut := range frame.mutations {
			key := mut.pos.Agency + ":" + mut.pos.VehicleID
			merged[key] = mut.pos
		}
		out := make([]gtfs.VehiclePosition, 0, len(merged))
		for _, pos := range merged {
			if _, ok := agencySet[pos.Agency]; ok {
				out = append(out, pos)
			}
		}
		prog := ReplayProgress{
			Frame: i + 1,
			Total: len(frames),
			At:    frame.at,
		}
		if err := onFrame(out, prog); err != nil {
			return err
		}
		if i+1 < len(frames) {
			if err := sleepCtx(ctx, delay); err != nil {
				return err
			}
		}
	}
	return nil
}

type replayFrameBucket struct {
	at        time.Time
	mutations []replayMutation
}

func (p *Pipeline) buildReplayFrames(ctx context.Context, opts ReplayOptions) ([]replayFrameBucket, error) {
	region, err := gtfs.RegionByID(opts.RegionID)
	if err != nil {
		return nil, err
	}

	var mutations []replayMutation
	for _, agencyID := range region.Agencies {
		aw, ok := p.agencies[agencyID]
		if !ok {
			continue
		}
		if err := pipeline.RefreshReader(ctx, aw.Reader); err != nil {
			return nil, err
		}
		_, err := pipeline.DrainChangeFeed(ctx, aw.DB, func(ch isledb.Change) error {
			if ch.Operation != isledb.ChangePut {
				return nil
			}
			pos, err := gtfs.ParseVehiclePosition(ch.Value)
			if err != nil {
				return nil
			}
			if !opts.From.IsZero() && pos.Timestamp.Before(opts.From) {
				return nil
			}
			if !opts.To.IsZero() && pos.Timestamp.After(opts.To) {
				return nil
			}
			mutations = append(mutations, replayMutation{
				at:  pos.Timestamp.Truncate(time.Second),
				pos: pos,
			})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("change feed %s: %w", agencyID, err)
		}
	}

	sort.Slice(mutations, func(i, j int) bool {
		if !mutations[i].at.Equal(mutations[j].at) {
			return mutations[i].at.Before(mutations[j].at)
		}
		if mutations[i].pos.Agency != mutations[j].pos.Agency {
			return mutations[i].pos.Agency < mutations[j].pos.Agency
		}
		return mutations[i].pos.VehicleID < mutations[j].pos.VehicleID
	})

	if len(mutations) == 0 {
		return nil, nil
	}

	buckets := make([]replayFrameBucket, 0)
	var current replayFrameBucket
	for _, mut := range mutations {
		if len(current.mutations) == 0 || !current.at.Equal(mut.at) {
			if len(current.mutations) > 0 {
				buckets = append(buckets, current)
			}
			current = replayFrameBucket{at: mut.at}
		}
		current.mutations = append(current.mutations, mut)
	}
	if len(current.mutations) > 0 {
		buckets = append(buckets, current)
	}
	return buckets, nil
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
