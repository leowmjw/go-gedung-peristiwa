package demo

import (
	"strings"
	"testing"
	"time"

	demopkg "github.com/leow/go-gedung-peristiwa/internal/demo"
	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

func TestAgencyGroup(t *testing.T) {
	cases := map[string]string{
		"ktmb":                   "ktmb",
		"prasarana-rapid-bus-kl": "prasarana",
		"mybas-ipoh":             "mybas",
		"other":                  "mybas",
	}
	for agency, want := range cases {
		if got := agencyGroup(agency); got != want {
			t.Fatalf("agencyGroup(%q) = %q, want %q", agency, got, want)
		}
	}
}

func TestMustJSON(t *testing.T) {
	got := string(mustJSON("klang-valley"))
	if got != `"klang-valley"` {
		t.Fatalf("mustJSON string = %q, want quoted JSON without extra escapes", got)
	}
	got = string(mustJSON(10))
	if got != "10" {
		t.Fatalf("mustJSON int = %q, want 10", got)
	}
}

func TestLiveViewsMarksStale(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	positions := []gtfs.VehiclePosition{
		{Agency: "ktmb", VehicleID: "fresh", Timestamp: now.Add(-1 * time.Minute)},
		{Agency: "ktmb", VehicleID: "stale", Timestamp: now.Add(-demopkg.LiveMapFreshWindow - time.Minute)},
	}
	views := liveViews(positions, now)
	if len(views) != 2 {
		t.Fatalf("views = %d, want 2", len(views))
	}
	if views[0].Stale {
		t.Error("fresh vehicle should not be stale")
	}
	if !views[1].Stale {
		t.Error("vehicle past LiveMapFreshWindow should be stale")
	}
}

func TestReplayViewsNeverStale(t *testing.T) {
	positions := []gtfs.VehiclePosition{
		{Agency: "ktmb", VehicleID: "old", Timestamp: time.Unix(1, 0).UTC()},
	}
	views := replayViews(positions)
	if len(views) != 1 {
		t.Fatalf("views = %d, want 1", len(views))
	}
	if views[0].Stale {
		t.Error("replay frames must never be marked stale")
	}
}

// The live snapshot omits vehicles past LiveMapVisibleWindow, so the map script
// must evict markers that are no longer in the payload.
func TestIndexHTMLEvictsMissingMarkers(t *testing.T) {
	for _, want := range []string{
		"function dropMissing(seen)",
		"dropMissing(seen)",
		"const seen = new Set(list.map(v => v.id))",
		"delete markers[id]",
	} {
		if !strings.Contains(indexHTML, want) {
			t.Errorf("indexHTML missing %q; aged-out vehicles would linger on the map", want)
		}
	}
	if strings.Contains(indexHTML, "clearStaleMarkers") {
		t.Error("clearStaleMarkers was renamed to clearMarkersOutsideAgencies (it filters by agency, not staleness)")
	}
}
