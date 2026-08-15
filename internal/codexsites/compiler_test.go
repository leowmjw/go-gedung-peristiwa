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
	for _, want := range []string{"D1Database", "R2Bucket", "/api/ingest", "/api/poll", "/api/status", "api.data.gov.my", "decodeGTFS", "EVENTS.put", "vehicle_positions", "regionId:regionID"} {
		if !strings.Contains(string(worker), want) {
			t.Errorf("worker.ts missing %q", want)
		}
	}
	if !strings.Contains(string(worker), `event: '+event+'\ndata: `) {
		t.Error("worker.ts SSE formatter must contain actual newline escapes")
	}
	if strings.Contains(string(worker), `event: '+event+'\\ndata: `) {
		t.Error("worker.ts SSE formatter contains double-escaped newlines")
	}
	app, err := os.ReadFile(filepath.Join(out, "public/app.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"setInterval(()=>{void pollGTFS()},30000)", "pollInFlight", "refreshVehicles", "fetch('/api/status')"} {
		if !strings.Contains(string(app), want) {
			t.Errorf("app.js missing %q", want)
		}
	}
	if strings.Contains(string(app), "new EventSource('/api/vehicles/stream')") {
		t.Error("app.js should use short-lived projection fetches instead of a long-lived SSE stream")
	}
}
