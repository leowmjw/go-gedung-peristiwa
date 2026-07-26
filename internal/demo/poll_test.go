package demo_test

import (
	"testing"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/demo"
)

func TestPollCoordinatorDedupesSharedRegion(t *testing.T) {
	sessions := demo.NewSessionStore()
	sessions.Touch("a")
	sessions.Touch("b")
	sessions.Touch("c")

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
	sessions.Touch("a")
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

func TestPollCoordinatorUnionOfRegions(t *testing.T) {
	sessions := demo.NewSessionStore()
	sessions.Touch("a")
	_ = sessions.SetRegion("a", "johor")
	sessions.Touch("b")

	coord := demo.NewPollCoordinator(sessions, time.Minute)
	feeds, regions, err := coord.FeedsForScheduledPoll(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 2 {
		t.Fatalf("regions = %v", regions)
	}
	if len(feeds) != 3 {
		t.Fatalf("feeds = %d", len(feeds))
	}
}

func TestRegionViewerCounts(t *testing.T) {
	sessions := demo.NewSessionStore()
	sessions.Touch("a")
	sessions.Touch("b")
	sessions.Touch("c")
	_ = sessions.SetRegion("c", "johor")

	counts := sessions.RegionViewerCounts()
	if counts["klang-valley"] != 2 || counts["johor"] != 1 {
		t.Fatalf("counts = %v", counts)
	}
}
