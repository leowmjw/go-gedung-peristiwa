// Package sitessmoke generates the static Datastar page used to verify Codex
// Sites deployment without starting the transit demo or its dependencies.
package sitessmoke

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
)

const (
	HTMLFileName     = "datastar-sanity.html"
	DatastarFileName = "datastar-v1.0.2.js"
	LicenseFileName  = "datastar-LICENSE.txt"
	DatastarSHA256   = "2837d87acf6ee0ba8e4e63765926c25a98d63883b02f88be194a86b81d3fd24a"
)

//go:embed assets/datastar-v1.0.2.js assets/DATASTAR-LICENSE
var assets embed.FS

type region struct {
	ID    string
	Label string
}

type pageData struct {
	Regions []region
}

// Generate writes the complete static site into dir.
func Generate(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	data := pageData{Regions: []region{
		{ID: "klang-valley", Label: "Klang Valley"},
		{ID: "national", Label: "National"},
		{ID: "penang", Label: "Penang"},
		{ID: "johor", Label: "Johor"},
	}}

	var page bytes.Buffer
	if err := pageTemplate.Execute(&page, data); err != nil {
		return fmt.Errorf("render page: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, HTMLFileName), page.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write page: %w", err)
	}
	if err := copyAsset(dir, "assets/datastar-v1.0.2.js", DatastarFileName); err != nil {
		return err
	}
	return copyAsset(dir, "assets/DATASTAR-LICENSE", LicenseFileName)
}

func copyAsset(dir, source, name string) error {
	b, err := assets.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read embedded asset %s: %w", source, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		return fmt.Errorf("write asset %s: %w", name, err)
	}
	return nil
}

var pageTemplate = template.Must(template.New("sanity").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="description" content="A Go-generated Datastar sanity check for Gedung Peristiwa on Codex Sites.">
  <title>Gedung Peristiwa — Datastar Sanity</title>
  <script type="module" src="/` + DatastarFileName + `"></script>
  <style>
    :root {
      color-scheme: dark;
      --bg: #08111f;
      --panel: rgba(13, 27, 46, 0.88);
      --line: #29415f;
      --text: #eef7ff;
      --muted: #9bb0c7;
      --cyan: #59d8ff;
      --green: #56e39f;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 1.25rem;
      background:
        radial-gradient(circle at 20% 15%, rgba(31, 123, 178, 0.28), transparent 34rem),
        radial-gradient(circle at 85% 80%, rgba(31, 178, 126, 0.16), transparent 28rem),
        var(--bg);
      color: var(--text);
      font: 16px/1.5 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main {
      width: min(100%, 44rem);
      padding: clamp(1.5rem, 5vw, 3rem);
      border: 1px solid var(--line);
      border-radius: 1.5rem;
      background: var(--panel);
      box-shadow: 0 2rem 6rem rgba(0, 0, 0, 0.35);
      backdrop-filter: blur(18px);
    }
    .eyebrow {
      margin: 0 0 0.75rem;
      color: var(--cyan);
      font: 700 0.75rem/1.2 ui-monospace, SFMono-Regular, Menlo, monospace;
      letter-spacing: 0.14em;
      text-transform: uppercase;
    }
    h1 { margin: 0; font-size: clamp(2rem, 8vw, 4rem); line-height: 0.98; letter-spacing: -0.055em; }
    .lede { max-width: 36rem; margin: 1rem 0 2rem; color: var(--muted); }
    .status {
      display: flex;
      align-items: center;
      gap: 0.65rem;
      margin-bottom: 1.5rem;
      padding: 0.75rem 0.9rem;
      border: 1px solid var(--line);
      border-radius: 0.85rem;
      color: var(--muted);
      background: rgba(4, 12, 22, 0.45);
    }
    .status::before { content: ""; width: 0.65rem; height: 0.65rem; border-radius: 50%; background: #f5c451; }
    .status.active { color: var(--green); }
    .status.active::before { background: var(--green); box-shadow: 0 0 1rem rgba(86, 227, 159, 0.65); }
    label { display: grid; gap: 0.5rem; color: var(--muted); font-size: 0.88rem; }
    select, button {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 0.85rem;
      padding: 0.85rem 1rem;
      color: var(--text);
      background: #0b192a;
      font: inherit;
    }
    select:focus-visible, button:focus-visible { outline: 3px solid rgba(89, 216, 255, 0.4); outline-offset: 2px; }
    .selection { margin: 1rem 0 1.5rem; font-size: 1.15rem; }
    button { cursor: pointer; color: #03111b; background: var(--cyan); border-color: transparent; font-weight: 750; }
    button:hover { filter: brightness(1.08); }
    .details { margin-top: 1rem; padding: 1rem; border-left: 3px solid var(--green); color: var(--muted); background: rgba(86, 227, 159, 0.07); }
    .details strong { color: var(--text); }
    footer { margin-top: 2rem; color: var(--muted); font: 0.75rem/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; }
  </style>
</head>
<body>
  <main
    data-signals="{region: 'klang-valley', details: false, ready: false}"
    data-init="$ready = true"
  >
    <p class="eyebrow">Codex Sites deployment probe</p>
    <h1>Gedung<br>Peristiwa</h1>
    <p class="lede">A static page rendered by Go and made interactive using declarative Datastar attributes only.</p>

    <div class="status" data-class:active="$ready" role="status" aria-live="polite">
      <span data-text="$ready ? 'Datastar active' : 'Datastar pending'">Datastar pending</span>
    </div>

    <label for="region">
      Transit region
      <select id="region" name="region" data-bind:region>
        {{range .Regions}}<option value="{{.ID}}">{{.Label}}</option>{{end}}
      </select>
    </label>

    <p class="selection" aria-live="polite" data-text="'Selected region: ' + $region">Selected region: klang-valley</p>

    <button
      type="button"
      data-on:click="$details = !$details"
      data-attr:aria-expanded="$details"
      aria-controls="sanity-details"
      aria-expanded="false"
    >Toggle implementation details</button>

    <section id="sanity-details" class="details" data-show="$details">
      <strong>Sanity check passed.</strong> No map, storage, polling, SSE, or application JavaScript is involved.
    </section>

    <footer>Go 1.26 · Datastar 1.0.2 · owner-only deployment</footer>
  </main>
</body>
</html>
`))
