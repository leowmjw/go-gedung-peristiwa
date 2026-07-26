package demo

import (
	"sync"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

// PollCoordinator plans GTFS fetches so each region is downloaded at most once per
// interval, even when many browser sessions share the same region.
type PollCoordinator struct {
	mu       sync.Mutex
	sessions *SessionStore
	interval time.Duration
	lastPoll map[string]time.Time // region id -> last successful fetch
}

// NewPollCoordinator returns a coordinator bound to session state.
func NewPollCoordinator(sessions *SessionStore, interval time.Duration) *PollCoordinator {
	return &PollCoordinator{
		sessions: sessions,
		interval: interval,
		lastPoll: make(map[string]time.Time),
	}
}

// MarkPolled records successful polls for the given region ids.
func (c *PollCoordinator) MarkPolled(regionIDs []string, at time.Time) {
	if len(regionIDs) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range regionIDs {
		c.lastPoll[id] = at
	}
}

// FeedsForScheduledPoll returns feeds for in-use regions that are due for refresh.
func (c *PollCoordinator) FeedsForScheduledPoll(now time.Time) ([]gtfs.Feed, []string, error) {
	due := c.dueRegions(now, "")
	if len(due) == 0 {
		return nil, nil, nil
	}
	feeds, err := gtfs.FeedsForRegions(due)
	return feeds, due, err
}

// FeedsForRegionSwitch returns feeds for one region after a session switch.
// Returns nil feeds when the region was fetched recently (pipeline already has data).
func (c *PollCoordinator) FeedsForRegionSwitch(now time.Time, regionID string) ([]gtfs.Feed, []string, error) {
	if regionID == "" {
		return c.FeedsForScheduledPoll(now)
	}
	if !c.isStale(regionID, now) {
		return nil, nil, nil
	}
	feeds, err := gtfs.FeedsForRegion(regionID)
	if err != nil {
		return nil, nil, err
	}
	return feeds, []string{regionID}, nil
}

func (c *PollCoordinator) dueRegions(now time.Time, only string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var candidates []string
	if only != "" {
		candidates = []string{only}
	} else {
		candidates = c.sessions.ActiveRegionIDs()
		if len(candidates) == 0 {
			candidates = []string{gtfs.DefaultRegionID}
		}
	}

	var due []string
	seen := make(map[string]struct{}, len(candidates))
	for _, id := range candidates {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if c.isStaleLocked(id, now) {
			due = append(due, id)
		}
	}
	return due
}

func (c *PollCoordinator) isStale(regionID string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isStaleLocked(regionID, now)
}

func (c *PollCoordinator) isStaleLocked(regionID string, now time.Time) bool {
	last, ok := c.lastPoll[regionID]
	return !ok || now.Sub(last) >= c.interval
}

// RegionViewerCounts returns how many sessions are viewing each region.
func (s *SessionStore) RegionViewerCounts() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := make(map[string]int)
	for _, id := range s.regions {
		counts[id]++
	}
	return counts
}
