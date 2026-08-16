// Package codexsites compiles the current Go demo configuration into an
// isolated Cloudflare Worker bundle for Codex Sites.
package codexsites

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

type siteConfig struct {
	Regions []siteRegion `json:"regions"`
	Feeds   []siteFeed   `json:"feeds"`
}

type siteRegion struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Center   [2]float64 `json:"center"`
	Zoom     int        `json:"zoom"`
	Agencies []string   `json:"agencies"`
}

type siteFeed struct {
	Agency string `json:"agency"`
	Region string `json:"region"`
	Group  string `json:"group"`
	URL    string `json:"url"`
}

// Generate writes a complete, disposable Codex Sites bundle to out.
func Generate(out string) error {
	regions := gtfs.AllRegions()
	feeds := gtfs.AllFeeds()
	cfg := siteConfig{
		Regions: make([]siteRegion, 0, len(regions)),
		Feeds:   make([]siteFeed, 0, len(feeds)),
	}
	for _, r := range regions {
		cfg.Regions = append(cfg.Regions, siteRegion{
			ID: r.ID, Label: r.Label, Center: r.Center, Zoom: r.Zoom,
			Agencies: append([]string(nil), r.Agencies...),
		})
	}
	for _, f := range feeds {
		cfg.Feeds = append(cfg.Feeds, siteFeed{Agency: f.Agency, Region: f.Region, Group: f.Group, URL: f.URL})
	}
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal site config: %w", err)
	}

	public := filepath.Join(out, "public")
	if err := os.MkdirAll(public, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	worker := strings.Replace(workerTS, "__REGIONS_JSON__", string(configJSON), 1)
	files := map[string]string{
		"public/index.html":  indexHTML,
		"public/replay.html": replayHTML,
		"public/config.js":   "window.SITE_CONFIG = " + string(configJSON) + ";\n",
		"public/app.css":     appCSS,
		"public/app.js":      appJS,
		"public/replay.js":   replayJS,
		"schema.sql":         schemaSQL,
		"wrangler.jsonc":     wranglerJSONC,
		"README.md":          readmeMD,
		"worker.ts":          worker,
	}
	for name, contents := range files {
		path := filepath.Join(out, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", name, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}
