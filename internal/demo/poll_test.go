package demo_test

import (
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
)

func view(s *demo.SessionStore, ids ...string) {
	for _, id := range ids {
		s.Touch(id)
		s.AcquireLive(id)
	}
}

func TestPollCoordinatorDedupesSharedRegion(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "a")
	view(sessions, "b")
	view(sessions, "c")

	coord := demo.NewPollCoordinator(sessions, time.Minute)
	now := time.Now()

	feeds, regions, err := coord.FeedsForScheduledPoll(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 || regions[0] != "klang-valley" {
		t.Fatalf("regions = %v", regions)
	}
	if len(feeds) != 2 {
		t.Fatalf("feeds = %d", len(feeds))
	}

	coord.MarkPolled(regions, now)

	feeds, regions, err = coord.FeedsForScheduledPoll(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 0 {
		t.Fatalf("expected no feeds while fresh, got %d regions=%v", len(feeds), regions)
	}
}

func TestPollCoordinatorRegionSwitchSkipsFresh(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "a")
	coord := demo.NewPollCoordinator(sessions, time.Minute)
	now := time.Now()
	coord.MarkPolled([]string{"johor"}, now)

	feeds, regions, err := coord.FeedsForRegionSwitch(now, "johor")
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 0 || len(regions) != 0 {
		t.Fatalf("expected skip, feeds=%d regions=%v", len(feeds), regions)
	}
}

func TestPollCoordinatorOneRegionPerTick(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "a")
	_ = sessions.SetRegion("a", "johor")
	view(sessions, "b")

	coord := demo.NewPollCoordinator(sessions, time.Minute)
	now := time.Now()

	seen := make(map[string]int)
	for i := range 2 {
		feeds, regions, err := coord.FeedsForScheduledPoll(now)
		if err != nil {
			t.Fatal(err)
		}
		if len(regions) != 1 {
			t.Fatalf("tick %d regions = %v, want exactly 1", i, regions)
		}
		if i == 0 && regions[0] != "klang-valley" {
			t.Fatalf("klang-valley should win over johor, got %s", regions[0])
		}
		seen[regions[0]]++
		if seen[regions[0]] > 1 {
			t.Fatalf("region %s served twice before the other due region", regions[0])
		}
		if len(feeds) == 0 {
			t.Fatalf("tick %d empty feeds", i)
		}
		coord.MarkPolled(regions, now)
	}
	if seen["johor"] != 1 || seen["klang-valley"] != 1 {
		t.Fatalf("seen = %v", seen)
	}

	feeds, regions, err := coord.FeedsForScheduledPoll(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 0 || len(regions) != 0 {
		t.Fatalf("expected none while both fresh, feeds=%d regions=%v", len(feeds), regions)
	}
}

func TestPollCoordinatorPriorityBeatsOlderOtherRegion(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "a")
	_ = sessions.SetRegion("a", "johor")
	view(sessions, "b")

	coord := demo.NewPollCoordinator(sessions, time.Minute)
	now := time.Now()
	// Both must have waited past the 30s upstream refresh floor to be "due".
	coord.MarkPolled([]string{"johor"}, now.Add(-35*time.Second))
	coord.MarkPolled([]string{"klang-valley"}, now.Add(-31*time.Second))

	_, regions, err := coord.FeedsForScheduledPoll(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 || regions[0] != "klang-valley" {
		t.Fatalf("want klang-valley over older johor, got %v", regions)
	}
}

func TestPollCoordinatorPriorityKlangValleyThenPenang(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "kv")
	view(sessions, "pg")
	_ = sessions.SetRegion("pg", "penang")
	view(sessions, "jh")
	_ = sessions.SetRegion("jh", "johor")

	coord := demo.NewPollCoordinator(sessions, time.Minute)
	now := time.Now()

	_, regions, err := coord.FeedsForScheduledPoll(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 || regions[0] != "klang-valley" {
		t.Fatalf("first = %v", regions)
	}
	coord.MarkPolled(regions, now)

	_, regions, err = coord.FeedsForScheduledPoll(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 || regions[0] != "penang" {
		t.Fatalf("second = %v", regions)
	}
	coord.MarkPolled(regions, now)

	_, regions, err = coord.FeedsForScheduledPoll(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 || regions[0] != "johor" {
		t.Fatalf("third = %v", regions)
	}
}

func TestPollCoordinatorStarvedRegionJumpsQueue(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "a")
	_ = sessions.SetRegion("a", "johor")
	view(sessions, "b")

	coord := demo.NewPollCoordinator(sessions, time.Minute)
	now := time.Now()
	// Starvation kicks in at 2x the (floored) 30s interval, i.e. 60s.
	coord.MarkPolled([]string{"johor"}, now.Add(-61*time.Second))
	coord.MarkPolled([]string{"klang-valley"}, now.Add(-30*time.Second))

	_, regions, err := coord.FeedsForScheduledPoll(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 || regions[0] != "johor" {
		t.Fatalf("starved johor should jump the queue, got %v", regions)
	}
}

func TestRegionViewerCounts(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "a")
	view(sessions, "b")
	view(sessions, "c")
	_ = sessions.SetRegion("c", "johor")

	counts := sessions.RegionViewerCounts()
	if counts["klang-valley"] != 2 || counts["johor"] != 1 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestPollCoordinatorUsesMinSessionInterval(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "fast")
	view(sessions, "slow")
	if err := sessions.SetPollSeconds("fast", 20); err != nil {
		t.Fatal(err)
	}
	if err := sessions.SetPollSeconds("slow", 30); err != nil {
		t.Fatal(err)
	}

	coord := demo.NewPollCoordinator(sessions, time.Minute)
	now := time.Now()
	coord.MarkPolled([]string{"klang-valley"}, now)

	// The fastest viewer asked for 20s, but the upstream GTFS refresh floor
	// (30s) wins: data.gov.my doesn't publish any faster than that, so
	// fetching sooner would just re-download identical data.
	feeds, _, err := coord.FeedsForScheduledPoll(now.Add(29 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 0 {
		t.Fatalf("expected still fresh at 29s (below the 30s upstream floor), feeds=%d", len(feeds))
	}

	feeds, regions, err := coord.FeedsForScheduledPoll(now.Add(30 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 || len(feeds) == 0 {
		t.Fatalf("expected due at 30s, regions=%v feeds=%d", regions, len(feeds))
	}
}

func TestPollCoordinatorHonorsSlowerInterval(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "a")
	if err := sessions.SetPollSeconds("a", 30); err != nil {
		t.Fatal(err)
	}
	coord := demo.NewPollCoordinator(sessions, time.Minute)
	now := time.Now()
	coord.MarkPolled([]string{"klang-valley"}, now)

	feeds, _, err := coord.FeedsForScheduledPoll(now.Add(10 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 0 {
		t.Fatalf("expected still fresh at 10s with 30s interval, feeds=%d", len(feeds))
	}

	_, regions, err := coord.FeedsForScheduledPoll(now.Add(30 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected due at 30s, regions=%v", regions)
	}
}

func TestPollCoordinatorDefaultTenSeconds(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "a")
	coord := demo.NewPollCoordinator(sessions, time.Minute)
	now := time.Now()
	coord.MarkPolled([]string{"klang-valley"}, now)

	// The session's default local interval is 10s, but the upstream GTFS
	// refresh floor (30s) governs when we actually re-fetch.
	feeds, _, err := coord.FeedsForScheduledPoll(now.Add(29 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 0 {
		t.Fatal("expected still fresh at 29s (below the 30s upstream floor)")
	}

	_, regions, err := coord.FeedsForScheduledPoll(now.Add(30 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected due at 30s, regions=%v", regions)
	}
}

func TestNormalizePollSeconds(t *testing.T) {
	for _, n := range []int{10, 20, 30} {
		got, err := demo.NormalizePollSeconds(n)
		if err != nil || got != n {
			t.Fatalf("NormalizePollSeconds(%d) = %d, %v", n, got, err)
		}
	}
	if _, err := demo.NormalizePollSeconds(5); err == nil {
		t.Fatal("expected error for 5")
	}
}

func TestNewPollCoordinatorZeroIntervalDefaults(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "a")
	coord := demo.NewPollCoordinator(sessions, 0)
	now := time.Now()
	coord.MarkPolled([]string{"klang-valley"}, now)
	feeds, _, err := coord.FeedsForScheduledPoll(now.Add(29 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 0 {
		t.Fatal("expected still fresh at 29s (below the 30s upstream floor)")
	}
	_, regions, err := coord.FeedsForScheduledPoll(now.Add(30 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 {
		t.Fatalf("expected due at 30s, regions=%v", regions)
	}
}

func TestPollCoordinatorNoViewersDoesNotPoll(t *testing.T) {
	coord := demo.NewPollCoordinator(demo.NewSessionStore(), time.Second)
	feeds, regions, err := coord.FeedsForScheduledPoll(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 0 || len(regions) != 0 {
		t.Fatalf("expected no poll without viewers, feeds=%d regions=%v", len(feeds), regions)
	}
}

func TestPollCoordinatorOnlyLiveSelectedRegions(t *testing.T) {
	sessions := demo.NewSessionStore()
	view(sessions, "kv")
	view(sessions, "pg1")
	view(sessions, "pg2")
	_ = sessions.SetRegion("pg1", "penang")
	_ = sessions.SetRegion("pg2", "penang")
	sessions.Touch("stale-east")
	_ = sessions.SetRegion("stale-east", "east-coast")

	coord := demo.NewPollCoordinator(sessions, time.Minute)
	live := map[string]bool{}
	for _, id := range sessions.LiveRegionIDs() {
		live[id] = true
	}
	if !live["klang-valley"] || !live["penang"] || live["east-coast"] {
		t.Fatalf("live poll list = %v, want klang-valley+penang without east-coast", sessions.LiveRegionIDs())
	}

	now := time.Now()
	seen := map[string]int{}
	for range 2 {
		_, regions, err := coord.FeedsForScheduledPoll(now)
		if err != nil {
			t.Fatal(err)
		}
		if len(regions) != 1 {
			t.Fatalf("regions = %v", regions)
		}
		if regions[0] == "east-coast" {
			t.Fatal("closed east-coast tab must not be polled")
		}
		seen[regions[0]]++
		coord.MarkPolled(regions, now)
	}
	if seen["klang-valley"] != 1 || seen["penang"] != 1 {
		t.Fatalf("seen = %v", seen)
	}

	_, regions, err := coord.FeedsForScheduledPoll(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 0 {
		t.Fatalf("expected no more live regions, got %v", regions)
	}
}
