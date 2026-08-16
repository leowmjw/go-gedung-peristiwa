package demo_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
)

func TestSessionStoreDefault(t *testing.T) {
	s := demo.NewSessionStore()
	s.Touch("browser-a")
	if s.Region("browser-a") != "klang-valley" {
		t.Fatalf("region = %q", s.Region("browser-a"))
	}
}

func TestSessionStoreSet(t *testing.T) {
	s := demo.NewSessionStore()
	s.Touch("browser-a")
	if err := s.SetRegion("browser-a", "johor"); err != nil {
		t.Fatal(err)
	}
	if s.Region("browser-a") != "johor" {
		t.Fatalf("region = %q", s.Region("browser-a"))
	}
}

func TestSessionStoreIndependentSessions(t *testing.T) {
	s := demo.NewSessionStore()
	s.Touch("a")
	s.Touch("b")
	if err := s.SetRegion("a", "johor"); err != nil {
		t.Fatal(err)
	}
	if s.Region("a") != "johor" {
		t.Fatalf("a = %q", s.Region("a"))
	}
	if s.Region("b") != "klang-valley" {
		t.Fatalf("b = %q", s.Region("b"))
	}
}

func TestSessionStoreSetUnknown(t *testing.T) {
	s := demo.NewSessionStore()
	s.Touch("a")
	if err := s.SetRegion("a", "nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSessionStoreActiveFeeds(t *testing.T) {
	s := demo.NewSessionStore()
	s.Touch("a")
	feeds, err := s.ActiveFeeds("a")
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 2 {
		t.Fatalf("feeds = %d", len(feeds))
	}
}

func TestSessionStoreFeedsToPollUnion(t *testing.T) {
	s := demo.NewSessionStore()
	s.Touch("a")
	s.AcquireLive("a")
	s.Touch("b")
	s.AcquireLive("b")
	if err := s.SetRegion("a", "johor"); err != nil {
		t.Fatal(err)
	}
	feeds, err := s.FeedsToPoll()
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 3 {
		t.Fatalf("feeds = %d, want 3 (johor + klang valley)", len(feeds))
	}
}

func TestSessionStorePersistRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	s1, err := demo.OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s1.Touch("browser-a")
	if err := s1.SetRegion("browser-a", "johor"); err != nil {
		t.Fatal(err)
	}
	if err := s1.SetPollSeconds("browser-a", 20); err != nil {
		t.Fatal(err)
	}

	s2, err := demo.OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Region("browser-a") != "johor" {
		t.Fatalf("after reload region = %q", s2.Region("browser-a"))
	}
	if s2.PollSeconds("browser-a") != 20 {
		t.Fatalf("after reload poll = %d", s2.PollSeconds("browser-a"))
	}
}

func TestSessionStoreLoadsLegacyRegionString(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte(`{"sessions":{"browser-a":"johor"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := demo.OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if ids := s.ActiveRegionIDs(); len(ids) != 0 {
		t.Fatalf("legacy session without lastSeen should be expired, got %v", ids)
	}
}

func TestSessionStoreLoadsInvalidPollSecondsAsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	body := `{"sessions":{"browser-a":{"region":"johor","pollSeconds":5,"lastSeen":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := demo.OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Region("browser-a") != "johor" {
		t.Fatalf("region = %q", s.Region("browser-a"))
	}
	if s.PollSeconds("browser-a") != demo.DefaultPollSeconds {
		t.Fatalf("poll = %d", s.PollSeconds("browser-a"))
	}
}

func TestSessionStorePollSecondsDefaultAndSet(t *testing.T) {
	s := demo.NewSessionStore()
	s.Touch("a")
	if s.PollSeconds("a") != demo.DefaultPollSeconds {
		t.Fatalf("default poll = %d", s.PollSeconds("a"))
	}
	if err := s.SetPollSeconds("a", 30); err != nil {
		t.Fatal(err)
	}
	if s.PollSeconds("a") != 30 {
		t.Fatalf("poll = %d", s.PollSeconds("a"))
	}
	s.Touch("b")
	if s.PollSeconds("b") != demo.DefaultPollSeconds {
		t.Fatalf("b should keep default, got %d", s.PollSeconds("b"))
	}
}

func TestSessionStoreSetPollSecondsRejects(t *testing.T) {
	s := demo.NewSessionStore()
	s.Touch("a")
	if err := s.SetPollSeconds("a", 7); err == nil {
		t.Fatal("expected error")
	}
}

func TestSessionStoreMinPollIntervalForRegion(t *testing.T) {
	s := demo.NewSessionStore()
	s.Touch("fast")
	s.AcquireLive("fast")
	s.Touch("slow")
	s.AcquireLive("slow")
	_ = s.SetRegion("slow", "johor")
	_ = s.SetPollSeconds("fast", 20)
	_ = s.SetPollSeconds("slow", 30)

	if d := s.MinPollIntervalForRegion("klang-valley"); d != 20*time.Second {
		t.Fatalf("klang-valley interval = %s", d)
	}
	if d := s.MinPollIntervalForRegion("johor"); d != 30*time.Second {
		t.Fatalf("johor interval = %s", d)
	}
	if d := s.MinPollIntervalForRegion("sarawak"); d != 0 {
		t.Fatalf("unused region interval = %s", d)
	}
}

func TestSessionStoreConcurrentSet(t *testing.T) {
	s := demo.NewSessionStore()
	var wg sync.WaitGroup
	for i, id := range []string{"s1", "s2", "s3", "s4"} {
		wg.Add(1)
		go func(sid string, n int) {
			defer wg.Done()
			s.Touch(sid)
			regions := []string{"johor", "sarawak", "klang-valley", "national"}
			_ = s.SetRegion(sid, regions[n])
		}(id, i)
	}
	wg.Wait()
	if len(s.ActiveRegionIDs()) != 4 {
		t.Fatalf("active regions = %v", s.ActiveRegionIDs())
	}
}

func TestSessionStorePollUnionDedupesRegion(t *testing.T) {
	s := demo.NewSessionStore()
	s.Touch("kv")
	s.AcquireLive("kv")
	s.Touch("pg1")
	s.AcquireLive("pg1")
	s.Touch("pg2")
	s.AcquireLive("pg2")
	_ = s.SetRegion("pg1", "penang")
	_ = s.SetRegion("pg2", "penang")

	ids := s.LiveRegionIDs()
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if len(ids) != 2 || !seen["klang-valley"] || !seen["penang"] {
		t.Fatalf("poll list = %v, want [klang-valley penang]", ids)
	}
	feeds, err := s.FeedsToPoll()
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 3 {
		t.Fatalf("feeds = %d, want 3 (klang valley 2 + penang 1)", len(feeds))
	}
}

func TestSessionStoreExpiresIdleAfterFiveMinutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := demo.NewSessionStore()
		s.Touch("kv")
		s.Touch("pg")
		_ = s.SetRegion("pg", "penang")
		s.Touch("east")
		_ = s.SetRegion("east", "east-coast")

		time.Sleep(demo.SessionIdleTTL - time.Second)
		s.Touch("kv")
		s.Touch("pg")
		time.Sleep(2 * time.Second)

		ids := map[string]bool{}
		for _, id := range s.ActiveRegionIDs() {
			ids[id] = true
		}
		if !ids["klang-valley"] || !ids["penang"] {
			t.Fatalf("reporting sessions dropped: %v", s.ActiveRegionIDs())
		}
		if ids["east-coast"] {
			t.Fatalf("idle east-coast should be cleared, got %v", s.ActiveRegionIDs())
		}
	})
}

func TestSessionStoreDropsIdleOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	old := time.Now().Add(-demo.SessionIdleTTL - time.Second).UTC().Format(time.RFC3339Nano)
	body := `{"sessions":{"east":{"region":"east-coast","pollSeconds":10,"lastSeen":"` + old + `"}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := demo.OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if ids := s.ActiveRegionIDs(); len(ids) != 0 {
		t.Fatalf("idle session loaded: %v", ids)
	}
}
