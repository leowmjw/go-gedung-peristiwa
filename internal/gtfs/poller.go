package gtfs

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	gtfsrt "github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

const defaultPollTimeout = 30 * time.Second

// Poller fetches and parses GTFS-R vehicle position feeds.
type Poller struct {
	Client  *http.Client
	Backoff Backoff
}

// Backoff controls retry behavior on rate limits.
type Backoff struct {
	Initial time.Duration
	Max     time.Duration
}

func (b Backoff) duration(attempt int) time.Duration {
	if b.Initial <= 0 {
		b.Initial = time.Second
	}
	if b.Max <= 0 {
		b.Max = 30 * time.Second
	}
	d := b.Initial << attempt
	if d > b.Max {
		return b.Max
	}
	return d
}

// DefaultPoller returns a poller with sensible defaults.
func DefaultPoller() *Poller {
	return &Poller{
		Client: &http.Client{Timeout: defaultPollTimeout},
		Backoff: Backoff{
			Initial: time.Second,
			Max:     30 * time.Second,
		},
	}
}

// PollResult summarizes one feed poll.
type PollResult struct {
	Feed      Feed
	Positions []VehiclePosition
	Skipped   int
	Err       error
}

// Poll fetches one feed, parses protobuf, and returns validated positions.
func (p *Poller) Poll(ctx context.Context, feed Feed) ([]VehiclePosition, error) {
	res := p.pollFeed(ctx, feed)
	return res.Positions, res.Err
}

func (p *Poller) pollFeed(ctx context.Context, feed Feed) PollResult {
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}

	var lastErr error
	for attempt := range 4 {
		if err := ctx.Err(); err != nil {
			return PollResult{Feed: feed, Err: err}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
		if err != nil {
			return PollResult{Feed: feed, Err: err}
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			break
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return PollResult{Feed: feed, Err: readErr}
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = fmt.Errorf("rate limited (429)")
			wait := p.Backoff.duration(attempt)
			slog.Warn("gtfs rate limited", "agency", feed.Agency, "retry_in", wait)
			select {
			case <-ctx.Done():
				return PollResult{Feed: feed, Err: ctx.Err()}
			case <-time.After(wait):
				continue
			}
		}

		if resp.StatusCode != http.StatusOK {
			return PollResult{Feed: feed, Err: fmt.Errorf("http %d", resp.StatusCode)}
		}

		positions, skipped, err := parseFeedMessage(feed, body)
		return PollResult{Feed: feed, Positions: positions, Skipped: skipped, Err: err}
	}

	return PollResult{Feed: feed, Err: lastErr}
}

// PollAll polls all feeds concurrently and returns per-feed results.
func (p *Poller) PollAll(ctx context.Context, feeds []Feed) []PollResult {
	results := make([]PollResult, len(feeds))
	var wg sync.WaitGroup
	wg.Add(len(feeds))
	for i, feed := range feeds {
		go func(i int, feed Feed) {
			defer wg.Done()
			results[i] = p.pollFeed(ctx, feed)
		}(i, feed)
	}
	wg.Wait()
	return results
}

func parseFeedMessage(feed Feed, body []byte) ([]VehiclePosition, int, error) {
	msg := &gtfsrt.FeedMessage{}
	if err := proto.Unmarshal(body, msg); err != nil {
		return nil, 0, fmt.Errorf("unmarshal feed: %w", err)
	}

	var positions []VehiclePosition
	skipped := 0
	for _, entity := range msg.Entity {
		vp := entity.GetVehicle()
		if vp == nil || vp.Position == nil {
			continue
		}
		lat := float64(vp.Position.GetLatitude())
		lng := float64(vp.Position.GetLongitude())
		if !ValidMalaysiaBounds(lat, lng) {
			skipped++
			continue
		}

		vehicleID := ""
		if v := vp.Vehicle; v != nil {
			vehicleID = v.GetId()
			if vehicleID == "" {
				vehicleID = v.GetLabel()
			}
		}
		if vehicleID == "" {
			vehicleID = entity.GetId()
		}
		if vehicleID == "" {
			skipped++
			continue
		}

		ts := time.Now().UTC()
		if vp.Timestamp != nil {
			ts = time.Unix(int64(vp.GetTimestamp()), 0).UTC()
		}

		pos := VehiclePosition{
			Agency:    feed.Agency,
			VehicleID: vehicleID,
			Lat:       lat,
			Lng:       lng,
			Bearing:   float64(vp.Position.GetBearing()),
			Speed:     float64(vp.Position.GetSpeed()),
			Timestamp: ts,
		}
		if vp.Trip != nil {
			pos.Trip = vp.Trip.GetTripId()
			pos.Route = vp.Trip.GetRouteId()
		}
		positions = append(positions, pos)
	}
	return positions, skipped, nil
}

// ParseFeedBytes parses protobuf bytes without HTTP (for tests).
func ParseFeedBytes(feed Feed, body []byte) ([]VehiclePosition, int, error) {
	return parseFeedMessage(feed, body)
}
