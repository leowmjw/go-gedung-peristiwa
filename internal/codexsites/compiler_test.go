package codexsites

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateCompilesCurrentTransitConfig(t *testing.T) {
	out := t.TempDir()
	if err := Generate(out); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	for _, name := range []string{
		"public/index.html", "public/replay.html", "public/config.js", "public/app.css",
		"public/app.js", "public/replay.js", "worker.ts", "schema.sql", "wrangler.jsonc", "README.md",
	} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("generated %s: %v", name, err)
		}
	}

	config, err := os.ReadFile(filepath.Join(out, "public/config.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"klang-valley", "east-coast", "mybas-kuching"} {
		if !strings.Contains(string(config), want) {
			t.Errorf("config.js missing %q", want)
		}
	}
	worker, err := os.ReadFile(filepath.Join(out, "worker.ts"))
	if err != nil {
		t.Fatal(err)
	}
	workerSrc := string(worker)
	for _, want := range []string{
		"D1Database", "R2Bucket", "/api/ingest", "/api/poll", "/api/status", "/api/poll-interval",
		"api.data.gov.my", "decodeGTFS", "EVENTS.put", "vehicle_positions", "regionId:regionID",
		"regionAgencies", "SELECT DISTINCT r2_key", "statements.slice(i,i+90)",
		"region_poll_state", "poll_seconds", "skipped:true", "knownRegion",
		"poll interval must be 10, 20, or 30 seconds",
		"pollFeedsSequential", "claimRegionPoll", "fetchFeed", "HTTP 429", "gtfsFeedsFor",
		"FRESH_MS", "VISIBLE_MS", "timestamp_ms >= ?", "stale",
	} {
		if !strings.Contains(workerSrc, want) {
			t.Errorf("worker.ts missing %q", want)
		}
	}
	if !strings.Contains(workerSrc, `event: '+event+'\ndata: `) {
		t.Error("worker.ts SSE formatter must contain actual newline escapes")
	}
	if strings.Contains(workerSrc, `event: '+event+'\\ndata: `) {
		t.Error("worker.ts SSE formatter contains double-escaped newlines")
	}
	if strings.Contains(workerSrc, "const regions=new Set<string>();") {
		t.Error("worker.ts still uses per-region Set snapshots instead of regionAgencies")
	}
	if strings.Contains(workerSrc, "SELECT id,r2_key,at_ms FROM snapshots") {
		t.Error("worker.ts replay must DISTINCT r2_key so one R2 object is one frame")
	}
	if strings.Contains(workerSrc, "Promise.all(feeds.map") {
		t.Error("worker.ts must fetch GTFS feeds sequentially, not Promise.all over feed.url")
	}
	if strings.Contains(workerSrc, "/api/vehicles/stream") {
		t.Error("worker.ts must not expose live SSE; use short GET /api/vehicles + /api/status")
	}
	if strings.Contains(workerSrc, "liveStream") {
		t.Error("worker.ts must not include liveStream handler")
	}

	app, err := os.ReadFile(filepath.Join(out, "public/app.js"))
	if err != nil {
		t.Fatal(err)
	}
	appSrc := string(app)
	for _, want := range []string{
		"schedulePoll()", "pollInFlight", "refreshVehicles", "fetch('/api/status')",
		"fetch('/api/poll-interval'", "ALLOWED_POLL=[10,20,30]", "pollGTFS(false)",
		"document.hidden", "pollSeconds*1000",
	} {
		if !strings.Contains(appSrc, want) {
			t.Errorf("app.js missing %q", want)
		}
	}
	if strings.Contains(appSrc, "new EventSource('/api/vehicles/stream')") {
		t.Error("app.js should use short-lived projection fetches instead of a long-lived SSE stream")
	}
	if strings.Contains(appSrc, "setInterval(()=>{void pollGTFS()},30000)") {
		t.Error("app.js still hardcodes a 30s poll; session interval 10/20/30s is required")
	}
	if !strings.Contains(appSrc, "v.stale") {
		t.Error("app.js should gray out stale vehicles using v.stale")
	}
	// /api/vehicles omits vehicles past VISIBLE_MS, so markers missing from the
	// payload must be removed or aged-out vehicles linger on the map.
	if !strings.Contains(appSrc, "if(!seen.has(id)") {
		t.Error("app.js must evict markers absent from the latest /api/vehicles payload")
	}

	index, err := os.ReadFile(filepath.Join(out, "public/index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="poll-interval"`) {
		t.Error("index.html missing poll-interval control")
	}

	schema, err := os.ReadFile(filepath.Join(out, "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"poll_seconds INTEGER NOT NULL DEFAULT 10", "region_poll_state"} {
		if !strings.Contains(string(schema), want) {
			t.Errorf("schema.sql missing %q", want)
		}
	}

	wrangler, err := os.ReadFile(filepath.Join(out, "wrangler.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wrangler), `"compatibility_date": "2026-08-06"`) {
		t.Error("wrangler.jsonc compatibility_date must stay at 2026-08-06; wrangler 4.118.0 workerd rejects newer dates")
	}

	replay, err := os.ReadFile(filepath.Join(out, "public/replay.js"))
	if err != nil {
		t.Fatal(err)
	}
	replaySrc := string(replay)
	if !strings.Contains(replaySrc, "agencyColor") {
		t.Error("replay.js missing agency-colored markers from the Go replay page")
	}
	if !strings.Contains(replaySrc, "encodeURIComponent(current)") {
		t.Error("replay.js must send the raw region id, not a JSON-quoted string")
	}
}
