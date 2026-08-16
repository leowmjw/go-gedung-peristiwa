package demo

import (
	"html/template"
)

const replayHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Gedung Peristiwa — Historical Replay</title>
  <link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" />
  <script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
  <style>
    :root { --bg: #0d1117; --panel: #161b22; --text: #e6edf3; --muted: #8b949e; --border: #30363d; --accent: #58a6ff; }
    * { box-sizing: border-box; }
    body { margin: 0; font-family: system-ui, sans-serif; background: var(--bg); color: var(--text); }
    .layout { display: flex; height: 100vh; }
    aside { width: 300px; background: var(--panel); border-right: 1px solid var(--border); padding: 1rem; overflow-y: auto; flex-shrink: 0; }
    aside h1 { font-size: 1rem; margin: 0 0 0.25rem; }
    aside h2 { font-size: 0.85rem; color: var(--muted); margin: 1rem 0 0.5rem; text-transform: uppercase; letter-spacing: 0.05em; }
    #map-wrap { flex: 1; min-height: 0; position: relative; }
    #map { width: 100%; height: 100%; }
    .sub { color: var(--muted); font-size: 0.8rem; margin-bottom: 0.75rem; }
    .nav-link { font-size: 0.8rem; color: var(--accent); text-decoration: none; }
    .nav-link:hover { text-decoration: underline; }
    .region-btn { display: block; width: 100%; text-align: left; margin: 0.2rem 0; padding: 0.45rem 0.6rem; border: 1px solid var(--border); border-radius: 6px; background: transparent; color: var(--text); cursor: pointer; font-size: 0.85rem; }
    .region-btn:hover { border-color: var(--accent); }
    .region-btn.active { border-color: var(--accent); background: rgba(88,166,255,0.12); }
    .empty-msg { color: #d29922; font-size: 0.85rem; margin: 0.5rem 0; }
    .catalog-agency { margin-bottom: 0.75rem; font-size: 0.8rem; }
    .catalog-agency h3 { margin: 0 0 0.25rem; font-size: 0.8rem; color: var(--text); }
    .catalog-agency .muted { color: var(--muted); }
    .snap-row { font-family: ui-monospace, monospace; font-size: 0.68rem; color: var(--muted); margin: 0.1rem 0; }
    .controls { display: flex; flex-wrap: wrap; gap: 0.35rem; margin-top: 0.5rem; }
    .controls button, .controls select { font-size: 0.8rem; padding: 0.35rem 0.5rem; border-radius: 6px; border: 1px solid var(--border); background: var(--panel); color: var(--text); cursor: pointer; }
    .controls button:disabled { opacity: 0.5; cursor: not-allowed; }
    .status { font-size: 0.8rem; color: var(--muted); margin-top: 0.5rem; }
    .status.ok { color: #3fb950; }
    .status.err { color: #f85149; }
    .notes { font-size: 0.7rem; color: var(--muted); margin-top: 0.75rem; }
    .notes li { margin: 0.2rem 0; }
  </style>
</head>
<body>
  <div class="layout">
    <aside>
      <h1>Historical replay</h1>
      <p class="sub"><a class="nav-link" href="/">← Live map</a></p>
      <p class="sub" id="region-subtitle">{{.ActiveRegion.Label}}</p>
      <h2>Region</h2>
      <div id="region-list">
        {{range .Regions}}
        <button type="button" class="region-btn{{if eq .ID $.ActiveRegion.ID}} active{{end}}" data-region="{{.ID}}" onclick="window.switchRegion('{{.ID}}')">{{.Label}}</button>
        {{end}}
      </div>
      <h2>Snapshots in S3</h2>
      <div id="catalog-empty" class="empty-msg" style="display:none"></div>
      <div id="catalog"></div>
      <h2>Playback</h2>
      <div class="controls">
        <button type="button" id="btn-play">Play</button>
        <button type="button" id="btn-stop" disabled>Stop</button>
        <select id="speed" aria-label="Speed">
          <option value="1">1×</option>
          <option value="10" selected>10×</option>
          <option value="60">60×</option>
        </select>
      </div>
      <p id="replay-status" class="status">Select a region to load catalog.</p>
      <ul class="notes" id="catalog-notes"></ul>
    </aside>
    <div id="map-wrap">
      <div id="map"></div>
    </div>
  </div>
  <script>
    const activeRegion = {{mustJSON .ActiveRegion.ID}};
    const activeLabel = {{mustJSON .ActiveRegion.Label}};
    const map = L.map('map', { preferCanvas: true }).setView(
      [{{index .ActiveRegion.Center 0}}, {{index .ActiveRegion.Center 1}}],
      {{.ActiveRegion.Zoom}}
    );
    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '&copy; OpenStreetMap'
    }).addTo(map);

    const markers = {};
    const groupColors = { ktmb: '#f85149', prasarana: '#58a6ff', mybas: '#3fb950' };

    function agencyColor(agency) {
      if (agency === 'ktmb') return groupColors.ktmb;
      if (agency.startsWith('prasarana')) return groupColors.prasarana;
      return groupColors.mybas;
    }

    function clearMarkers() {
      for (const id of Object.keys(markers)) {
        map.removeLayer(markers[id]);
        delete markers[id];
      }
    }

    function applyVehicles(list) {
      const seen = new Set();
      for (const v of list) {
        seen.add(v.id);
        const latlng = [v.lat, v.lng];
        if (markers[v.id]) {
          markers[v.id].setLatLng(latlng);
        } else {
          markers[v.id] = L.circleMarker(latlng, {
            radius: 5,
            color: agencyColor(v.agency),
            fillOpacity: 0.85
          }).addTo(map).bindPopup(v.agency + ' — ' + (v.route || v.id));
        }
      }
      for (const id of Object.keys(markers)) {
        if (!seen.has(id)) {
          map.removeLayer(markers[id]);
          delete markers[id];
        }
      }
    }

    function setRegionButtons(id) {
      document.querySelectorAll('.region-btn').forEach(btn => {
        btn.classList.toggle('active', btn.getAttribute('data-region') === id);
      });
    }

    function renderCatalog(data) {
      const emptyEl = document.getElementById('catalog-empty');
      const catEl = document.getElementById('catalog');
      const notesEl = document.getElementById('catalog-notes');
      catEl.innerHTML = '';
      notesEl.innerHTML = '';
      if (data.empty) {
        emptyEl.style.display = 'block';
        emptyEl.textContent = data.message || 'No change-feed history for this region.';
      } else {
        emptyEl.style.display = 'none';
      }
      (data.notes || []).forEach(n => {
        const li = document.createElement('li');
        li.textContent = n;
        notesEl.appendChild(li);
      });
      (data.agencies || []).forEach(ag => {
        const block = document.createElement('div');
        block.className = 'catalog-agency';
        const title = document.createElement('h3');
        title.textContent = ag.agency + ' (' + ag.count + ')';
        block.appendChild(title);
        if (!ag.count) {
          const p = document.createElement('p');
          p.className = 'muted';
          p.textContent = 'No data yet';
          block.appendChild(p);
        } else {
          (ag.entries || []).slice(0, 8).forEach(e => {
            const row = document.createElement('div');
            row.className = 'snap-row';
            const from = e.from ? new Date(e.from).toISOString() : '—';
            const to = e.to ? new Date(e.to).toISOString() : '—';
            row.textContent = from + ' → ' + to + ' · ' + e.changeCount + ' changes';
            block.appendChild(row);
          });
        }
        catEl.appendChild(block);
      });
    }

    async function loadCatalog(regionId) {
      const status = document.getElementById('replay-status');
      status.textContent = 'Loading catalog…';
      status.className = 'status';
      try {
        const res = await fetch('/api/replay/catalog?region=' + encodeURIComponent(regionId));
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        renderCatalog(data);
        status.textContent = data.empty
          ? 'No change-feed history to replay.'
          : 'Loaded ' + data.total + ' change(s). Press Play.';
        status.className = data.empty ? 'status err' : 'status ok';
      } catch (e) {
        status.textContent = 'Catalog failed: ' + e.message;
        status.className = 'status err';
      }
    }

    let currentRegion = activeRegion;

    window.switchRegion = async function(id) {
      currentRegion = id;
      setRegionButtons(id);
      const res = await fetch('/api/region', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id })
      });
      if (!res.ok) return;
      const data = await res.json();
      const r = data.region;
      document.getElementById('region-subtitle').textContent = r.label;
      map.flyTo([r.center[0], r.center[1]], r.zoom, { duration: 0.8 });
      clearMarkers();
      stopReplay();
      await loadCatalog(id);
    };

    let replaySource = null;

    function stopReplay() {
      if (replaySource) {
        replaySource.close();
        replaySource = null;
      }
      document.getElementById('btn-play').disabled = false;
      document.getElementById('btn-stop').disabled = true;
    }

    function startReplay() {
      stopReplay();
      const speed = document.getElementById('speed').value;
      const status = document.getElementById('replay-status');
      status.textContent = 'Connecting replay stream…';
      const url = '/api/replay/stream?region=' + encodeURIComponent(currentRegion)
        + '&speed=' + encodeURIComponent(speed);
      replaySource = new EventSource(url);
      document.getElementById('btn-play').disabled = true;
      document.getElementById('btn-stop').disabled = false;

      replaySource.addEventListener('vehicles', e => {
        applyVehicles(JSON.parse(e.data));
      });
      replaySource.addEventListener('progress', e => {
        const p = JSON.parse(e.data);
        if (p.status === 'starting') return;
        status.textContent = 'Frame ' + p.frame + ' / ' + p.total + (p.at ? ' · ' + p.at : '');
        status.className = 'status ok';
      });
      replaySource.addEventListener('done', () => {
        status.textContent = 'Replay complete.';
        stopReplay();
      });
      replaySource.addEventListener('error', e => {
        if (e.data) {
          try {
            const err = JSON.parse(e.data);
            status.textContent = err.message || 'Replay error';
          } catch (_) {
            status.textContent = 'Replay error';
          }
        }
        status.className = 'status err';
        stopReplay();
      });
      replaySource.onerror = () => {
        if (replaySource && replaySource.readyState === EventSource.CLOSED) {
          stopReplay();
        }
      };
    }

    document.getElementById('btn-play').addEventListener('click', startReplay);
    document.getElementById('btn-stop').addEventListener('click', stopReplay);

    loadCatalog(activeRegion);
  </script>
</body>
</html>`

var replayTmpl = template.Must(func() (*template.Template, error) {
	return template.New("replay").Funcs(template.FuncMap{
		"mustJSON": mustJSON,
	}).Parse(replayHTML)
}())
