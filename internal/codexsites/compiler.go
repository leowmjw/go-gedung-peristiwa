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
	// One R2 object is one replay frame. Snapshot catalog rows remain per
	// agency for the existing UI, while replay reads each batch only once.
	worker = strings.Replace(worker, "const regions=new Set<string>();", "const regionAgencies=new Map<string,Set<string>>();", 1)
	worker = strings.Replace(worker, "regions.add(r.id);", "if(!regionAgencies.has(r.id))regionAgencies.set(r.id,new Set<string>());regionAgencies.get(r.id)!.add(v.agency);", 1)
	worker = strings.Replace(worker, "for(const regionID of regions){for(const agency of agenciesFor(regionID))", "for(const [regionID,agencies] of regionAgencies){for(const agency of agencies)", 1)
	worker = strings.Replace(worker, "SELECT id,r2_key,at_ms FROM snapshots", "SELECT DISTINCT r2_key,at_ms FROM snapshots", 1)
	worker = strings.Replace(worker, "const {sid,regionID}=await session(request,env);if(path==='/api/regions')", "const {sid,regionID}=await session(request,env);if(path==='/api/poll'&&request.method==='POST')return withCookie(await pollGTFS(env,url.searchParams.get('region')||regionID),sid);if(path==='/api/regions')", 1)
	worker = strings.Replace(worker, "if(path==='/api/vehicles')return withCookie(json(await latest(env,regionID)),sid);", "if(path==='/api/vehicles')return withCookie(json(await latest(env,regionID)),sid);if(path==='/api/status')return withCookie(json(await stats(env,regionID)),sid);", 1)
	// D1 batches have a practical statement limit; keep large GTFS polls
	// deployable without changing the ingestion payload shape.
	worker = strings.Replace(worker, "if(statements.length)await env.DB.batch(statements);return json({accepted:positions.length,r2Key})", "for(let i=0;i<statements.length;i+=90)await env.DB.batch(statements.slice(i,i+90));return json({accepted:positions.length,r2Key})", 1)
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
