package demo_test

import (
	"sync"
	"testing"

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
	s.Touch("b")
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
