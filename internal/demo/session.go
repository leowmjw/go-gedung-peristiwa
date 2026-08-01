package demo

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"sync"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

// DefaultSessionStorePath is the default file for persisted demo browser sessions.
const DefaultSessionStorePath = "data/demo-sessions.json"

// SessionCookieName is the HTTP cookie used to identify a demo browser session.
const SessionCookieName = "demo_sid"

// SessionStore tracks per-browser region selection.
type SessionStore struct {
	mu      sync.RWMutex
	regions map[string]string // session id -> region id
	path    string            // empty = in-memory only
}

type sessionFile struct {
	Sessions map[string]string `json:"sessions"`
}

// NewSessionStore returns an in-memory session store (no persistence).
func NewSessionStore() *SessionStore {
	return &SessionStore{
		regions: make(map[string]string),
	}
}

// OpenSessionStore loads session regions from path (creates parent dirs on save).
// Missing file starts empty. Use NewSessionStore for tests without disk I/O.
func OpenSessionStore(path string) (*SessionStore, error) {
	s := &SessionStore{
		regions: make(map[string]string),
		path:    path,
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SessionStore) load() error {
	if s.path == "" {
		return nil
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(b) == 0 {
		return nil
	}
	var file sessionFile
	if err := json.Unmarshal(b, &file); err != nil {
		return err
	}
	if file.Sessions != nil {
		for sid, regionID := range file.Sessions {
			if _, err := gtfs.RegionByID(regionID); err != nil {
				continue
			}
			s.regions[sid] = regionID
		}
	}
	return nil
}

func (s *SessionStore) persist() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	file := sessionFile{Sessions: make(map[string]string, len(s.regions))}
	maps.Copy(file.Sessions, s.regions)
	b, err := json.Marshal(file)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
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
		_ = s.persistLocked()
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
	err := s.persistLocked()
	s.mu.Unlock()
	return err
}

func (s *SessionStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	file := sessionFile{Sessions: make(map[string]string, len(s.regions))}
	maps.Copy(file.Sessions, s.regions)
	b, err := json.Marshal(file)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
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
