package demo

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

// DefaultSessionStorePath is the default file for persisted demo browser sessions.
const DefaultSessionStorePath = "data/demo-sessions.json"

// SessionCookieName is the HTTP cookie used to identify a demo browser session.
const SessionCookieName = "demo_sid"

// SessionIdleTTL is how long a session may go without reporting before it is
// dropped from the poll union and cleared from the store.
const SessionIdleTTL = 5 * time.Minute

// SessionHeartbeatInterval is how often a live map SSE connection reports in.
const SessionHeartbeatInterval = time.Minute

type sessionState struct {
	Region      string    `json:"region"`
	PollSeconds int       `json:"pollSeconds,omitempty"`
	LastSeen    time.Time `json:"lastSeen"`
}

// SessionStore tracks per-browser region and poll-interval selection.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]sessionState
	live     map[string]int // session id -> live SSE refcount
	path     string         // empty = in-memory only
}

type sessionFile struct {
	Sessions map[string]json.RawMessage `json:"sessions"`
}

// NewSessionStore returns an in-memory session store (no persistence).
func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]sessionState),
		live:     make(map[string]int),
	}
}

// OpenSessionStore loads session regions from path (creates parent dirs on save).
// Missing file starts empty. Use NewSessionStore for tests without disk I/O.
func OpenSessionStore(path string) (*SessionStore, error) {
	s := &SessionStore{
		sessions: make(map[string]sessionState),
		live:     make(map[string]int),
		path:     path,
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
	now := time.Now()
	for sid, raw := range file.Sessions {
		st, ok := parseSessionValue(raw)
		if !ok {
			continue
		}
		if _, err := gtfs.RegionByID(st.Region); err != nil {
			continue
		}
		if sessionExpired(st, now) {
			continue
		}
		s.sessions[sid] = st
	}
	return nil
}

func parseSessionValue(raw json.RawMessage) (sessionState, bool) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil && asString != "" {
		return sessionState{Region: asString, PollSeconds: DefaultPollSeconds}, true
	}
	var st sessionState
	if err := json.Unmarshal(raw, &st); err != nil || st.Region == "" {
		return sessionState{}, false
	}
	st.PollSeconds = storedPollSeconds(st.PollSeconds)
	return st, true
}

func storedPollSeconds(n int) int {
	if v, err := NormalizePollSeconds(n); err == nil {
		return v
	}
	return DefaultPollSeconds
}

func sessionExpired(st sessionState, now time.Time) bool {
	if st.LastSeen.IsZero() {
		return true
	}
	return now.Sub(st.LastSeen) >= SessionIdleTTL
}

func (s *SessionStore) pruneExpiredLocked(now time.Time) bool {
	removed := false
	for sid, st := range s.sessions {
		if sessionExpired(st, now) {
			delete(s.sessions, sid)
			removed = true
		}
	}
	return removed
}

func (s *SessionStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	file := sessionFile{Sessions: make(map[string]json.RawMessage, len(s.sessions))}
	for sid, st := range s.sessions {
		if st.PollSeconds == 0 {
			st.PollSeconds = DefaultPollSeconds
		}
		b, err := json.Marshal(st)
		if err != nil {
			return err
		}
		file.Sessions[sid] = b
	}
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

func defaultState() sessionState {
	return sessionState{
		Region:      gtfs.DefaultRegionID,
		PollSeconds: DefaultPollSeconds,
		LastSeen:    time.Now(),
	}
}

func (s *SessionStore) stateLocked(sessionID string) sessionState {
	if st, ok := s.sessions[sessionID]; ok {
		if st.PollSeconds == 0 {
			st.PollSeconds = DefaultPollSeconds
		}
		return st
	}
	return defaultState()
}

// Touch records that a session is still reporting. New sessions start on the
// default region. Idle sessions past SessionIdleTTL are cleared.
func (s *SessionStore) Touch(sessionID string) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	st, ok := s.sessions[sessionID]
	if !ok {
		st = defaultState()
	}
	st.LastSeen = now
	s.sessions[sessionID] = st
	pruned := s.pruneExpiredLocked(now)
	if !ok || pruned {
		_ = s.persistLocked()
	}
}

// AcquireLive marks a session as a connected map viewer. Refcounted for multiple tabs.
func (s *SessionStore) AcquireLive(sessionID string) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.live[sessionID]++
}

// ReleaseLive drops one connected map viewer for a session.
func (s *SessionStore) ReleaseLive(sessionID string) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.live[sessionID] - 1
	if n <= 0 {
		delete(s.live, sessionID)
		return
	}
	s.live[sessionID] = n
}

// LiveRegionIDs is the union of regions connected map viewers have selected.
// Duplicate browsers on the same region appear once. Empty when nobody is watching.
func (s *SessionStore) LiveRegionIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pruneExpiredLocked(time.Now()) {
		_ = s.persistLocked()
	}
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for sid, n := range s.live {
		if n <= 0 {
			continue
		}
		st, ok := s.sessions[sid]
		if !ok || st.Region == "" || sessionExpired(st, time.Now()) {
			continue
		}
		if _, dup := seen[st.Region]; dup {
			continue
		}
		seen[st.Region] = struct{}{}
		ids = append(ids, st.Region)
	}
	return ids
}

// Region returns the active region for a session.
func (s *SessionStore) Region(sessionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pruneExpiredLocked(time.Now()) {
		_ = s.persistLocked()
	}
	return s.stateLocked(sessionID).Region
}

// SetRegion switches the active region for one session.
func (s *SessionStore) SetRegion(sessionID, regionID string) error {
	if _, err := gtfs.RegionByID(regionID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stateLocked(sessionID)
	st.Region = regionID
	st.LastSeen = time.Now()
	s.sessions[sessionID] = st
	s.pruneExpiredLocked(st.LastSeen)
	return s.persistLocked()
}

// PollSeconds returns the GTFS poll interval chosen for a session.
func (s *SessionStore) PollSeconds(sessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pruneExpiredLocked(time.Now()) {
		_ = s.persistLocked()
	}
	return s.stateLocked(sessionID).PollSeconds
}

// SetPollSeconds stores a session's GTFS poll interval (10, 20, or 30 seconds).
func (s *SessionStore) SetPollSeconds(sessionID string, seconds int) error {
	n, err := NormalizePollSeconds(seconds)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stateLocked(sessionID)
	st.PollSeconds = n
	st.LastSeen = time.Now()
	s.sessions[sessionID] = st
	s.pruneExpiredLocked(st.LastSeen)
	return s.persistLocked()
}

// MinPollIntervalForRegion is the fastest interval requested by reporting
// viewers of regionID. Returns 0 when no live session is viewing that region.
func (s *SessionStore) MinPollIntervalForRegion(regionID string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.pruneExpiredLocked(now) {
		_ = s.persistLocked()
	}
	min := time.Duration(0)
	for sid, n := range s.live {
		if n <= 0 {
			continue
		}
		st, ok := s.sessions[sid]
		if !ok || st.Region != regionID {
			continue
		}
		d := time.Duration(storedPollSeconds(st.PollSeconds)) * time.Second
		if min == 0 || d < min {
			min = d
		}
	}
	return min
}

// ActiveRegionIDs returns the union of regions selected by sessions that have
// reported within SessionIdleTTL. Duplicate browsers on the same region appear
// once. Empty when nobody is watching.
func (s *SessionStore) ActiveRegionIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pruneExpiredLocked(time.Now()) {
		_ = s.persistLocked()
	}
	if len(s.sessions) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(s.sessions))
	ids := make([]string, 0, len(s.sessions))
	for _, st := range s.sessions {
		if _, ok := seen[st.Region]; ok {
			continue
		}
		seen[st.Region] = struct{}{}
		ids = append(ids, st.Region)
	}
	return ids
}

// FeedsToPoll returns GTFS feeds for regions live map viewers are watching.
func (s *SessionStore) FeedsToPoll() ([]gtfs.Feed, error) {
	return gtfs.FeedsForRegions(s.LiveRegionIDs())
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
