package demo

import (
	"fmt"
	"sync"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

const (
	// DefaultPollSeconds is the live-map poll interval until a session picks another.
	DefaultPollSeconds = 10
	// DefaultPollInterval is DefaultPollSeconds as a duration.
	DefaultPollInterval = DefaultPollSeconds * time.Second
	// PollTickInterval is how often the scheduler checks whether any region is due.
	// It matches the fastest UI option so a 10s selection can take effect promptly.
	PollTickInterval = 10 * time.Second
)

// AllowedPollSeconds are the live-map GTFS poll interval choices.
var AllowedPollSeconds = []int{10, 20, 30}

// NormalizePollSeconds accepts 10, 20, or 30; anything else is an error.
func NormalizePollSeconds(seconds int) (int, error) {
	for _, n := range AllowedPollSeconds {
		if n == seconds {
			return n, nil
		}
	}
	return 0, fmt.Errorf("poll interval must be 10, 20, or 30 seconds")
}

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
	if interval <= 0 {
		interval = DefaultPollInterval
	}
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

// FeedsForScheduledPoll returns feeds for a single in-use region that is due.
// At most one region is chosen per call so each scheduler tick hits data.gov.my
// once (that region's feeds), even when several regions are stale.
func (c *PollCoordinator) FeedsForScheduledPoll(now time.Time) ([]gtfs.Feed, []string, error) {
	id := c.nextDueRegion(now)
	if id == "" {
		return nil, nil, nil
	}
	feeds, err := gtfs.FeedsForRegion(id)
	if err != nil {
		return nil, nil, err
	}
	return feeds, []string{id}, nil
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

const (
	pollPriorityKlangValley = 0
	pollPriorityPenang      = 1
	pollPriorityOther       = 2
)

func regionPollPriority(regionID string) int {
	switch regionID {
	case gtfs.DefaultRegionID:
		return pollPriorityKlangValley
	case "penang":
		return pollPriorityPenang
	default:
		return pollPriorityOther
	}
}

func (c *PollCoordinator) intervalLocked(regionID string) time.Duration {
	interval := c.sessions.MinPollIntervalForRegion(regionID)
	if interval <= 0 {
		return c.interval
	}
	return interval
}

// effectivePollPriorityLocked ranks due regions. Klang Valley, then Penang, then
// others. A non-priority region that has waited at least twice its interval is
// promoted so it cannot starve.
func (c *PollCoordinator) effectivePollPriorityLocked(regionID string, now time.Time) int {
	pri := regionPollPriority(regionID)
	if pri < pollPriorityOther {
		return pri
	}
	last, ok := c.lastPoll[regionID]
	if ok && now.Sub(last) >= 2*c.intervalLocked(regionID) {
		return pollPriorityKlangValley
	}
	return pri
}

// nextDueRegion picks at most one stale region per tick. Klang Valley and Penang
// win when due (highest churn). Other due regions wait unless they are starving.
func (c *PollCoordinator) nextDueRegion(now time.Time) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	candidates := c.sessions.LiveRegionIDs()
	if len(candidates) == 0 {
		return ""
	}

	best := ""
	var bestLast time.Time
	bestSeen := false
	bestPri := 0
	for _, id := range candidates {
		if !c.isStaleLocked(id, now) {
			continue
		}
		last, ok := c.lastPoll[id]
		pri := c.effectivePollPriorityLocked(id, now)
		if best == "" {
			best, bestLast, bestSeen, bestPri = id, last, ok, pri
			continue
		}
		if preferDueRegion(id, last, ok, pri, best, bestLast, bestSeen, bestPri) {
			best, bestLast, bestSeen, bestPri = id, last, ok, pri
		}
	}
	return best
}

func preferDueRegion(id string, last time.Time, seen bool, pri int, bestID string, bestLast time.Time, bestSeen bool, bestPri int) bool {
	if pri != bestPri {
		return pri < bestPri
	}
	if !seen && bestSeen {
		return true
	}
	if seen && !bestSeen {
		return false
	}
	if !seen && !bestSeen {
		return id < bestID
	}
	if last.Equal(bestLast) {
		return id < bestID
	}
	return last.Before(bestLast)
}

func (c *PollCoordinator) isStale(regionID string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isStaleLocked(regionID, now)
}

func (c *PollCoordinator) isStaleLocked(regionID string, now time.Time) bool {
	last, ok := c.lastPoll[regionID]
	return !ok || now.Sub(last) >= c.intervalLocked(regionID)
}

// RegionViewerCounts returns how many sessions are viewing each region.
func (s *SessionStore) RegionViewerCounts() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pruneExpiredLocked(time.Now()) {
		_ = s.persistLocked()
	}
	counts := make(map[string]int)
	for _, st := range s.sessions {
		counts[st.Region]++
	}
	return counts
}
