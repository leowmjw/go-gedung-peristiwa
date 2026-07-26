package demo

import (
	"sync"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

// SessionCookieName is the HTTP cookie used to identify a demo browser session.
const SessionCookieName = "demo_sid"

// SessionStore tracks per-browser region selection in memory.
type SessionStore struct {
	mu      sync.RWMutex
	regions map[string]string // session id -> region id
}

// NewSessionStore returns an empty in-memory session store.
func NewSessionStore() *SessionStore {
	return &SessionStore{
		regions: make(map[string]string),
	}
}

// Touch registers a session with the default region when new.
func (s *SessionStore) Touch(sessionID string) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.regions[sessionID]; !ok {
		s.regions[sessionID] = gtfs.DefaultRegionID
	}
}

// Region returns the active region for a session.
func (s *SessionStore) Region(sessionID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r, ok := s.regions[sessionID]; ok {
		return r
	}
	return gtfs.DefaultRegionID
}

// SetRegion switches the active region for one session.
func (s *SessionStore) SetRegion(sessionID, regionID string) error {
	if _, err := gtfs.RegionByID(regionID); err != nil {
		return err
	}
	s.mu.Lock()
	s.regions[sessionID] = regionID
	s.mu.Unlock()
	return nil
}

// ActiveRegionIDs returns the union of regions selected across sessions.
func (s *SessionStore) ActiveRegionIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.regions) == 0 {
		return []string{gtfs.DefaultRegionID}
	}
	seen := make(map[string]struct{}, len(s.regions))
	ids := make([]string, 0, len(s.regions))
	for _, id := range s.regions {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// FeedsToPoll returns GTFS feeds for all regions any session is viewing.
func (s *SessionStore) FeedsToPoll() ([]gtfs.Feed, error) {
	return gtfs.FeedsForRegions(s.ActiveRegionIDs())
}

// ActiveRegion returns the map region for a session.
func (s *SessionStore) ActiveRegion(sessionID string) gtfs.MapRegion {
	r, _ := gtfs.RegionByID(s.Region(sessionID))
	return r
}

// ActiveFeeds returns feeds for a session's active region.
func (s *SessionStore) ActiveFeeds(sessionID string) ([]gtfs.Feed, error) {
	return gtfs.FeedsForRegion(s.Region(sessionID))
}

// ActiveAgencies returns agency ids for a session's active region.
func (s *SessionStore) ActiveAgencies(sessionID string) (map[string]struct{}, error) {
	ids, err := gtfs.AgenciesForRegion(s.Region(sessionID))
	if err != nil {
		return nil, err
	}
	return gtfs.AgencySet(ids), nil
}
