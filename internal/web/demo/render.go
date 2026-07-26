package demo

// indexHTML is the single-page transit map (Leaflet, no build step).
const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Gedung Peristiwa — Malaysia Transit Live</title>
  <link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" />
  <script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
  <style>
    :root { --bg: #0d1117; --panel: #161b22; --text: #e6edf3; --muted: #8b949e; --border: #30363d; --accent: #58a6ff; }
    * { box-sizing: border-box; }
    body { margin: 0; font-family: system-ui, sans-serif; background: var(--bg); color: var(--text); }
    .layout { display: flex; height: 100vh; }
    aside { width: 280px; background: var(--panel); border-right: 1px solid var(--border); padding: 1rem; overflow-y: auto; flex-shrink: 0; }
    aside h1 { font-size: 1rem; margin: 0 0 0.5rem; }
    aside h2 { font-size: 0.85rem; color: var(--muted); margin: 1rem 0 0.5rem; text-transform: uppercase; letter-spacing: 0.05em; }
    #map-wrap { flex: 1; min-height: 0; position: relative; }
    #map { width: 100%; height: 100%; }
    .stats p { margin: 0.35rem 0; font-size: 0.9rem; }
    .stats span { font-weight: 600; }
    .sub { color: var(--muted); font-size: 0.8rem; margin-bottom: 1rem; }
    .status { font-size: 0.8rem; color: var(--muted); margin-top: 0.5rem; }
    .status.ok { color: #3fb950; }
    .status.err { color: #f85149; }
    .region-btn { display: block; width: 100%; text-align: left; margin: 0.2rem 0; padding: 0.45rem 0.6rem; border: 1px solid var(--border); border-radius: 6px; background: transparent; color: var(--text); cursor: pointer; font-size: 0.85rem; }
    .region-btn:hover { border-color: var(--accent); }
    .region-btn.active { border-color: var(--accent); background: rgba(88,166,255,0.12); }
    .tenant-list { font-size: 0.8rem; color: var(--muted); margin: 0; padding-left: 1rem; }
    .tenant-list li { margin: 0.15rem 0; }
    .debug-toggle { font-size: 0.8rem; margin-top: 1rem; }
    #ingest-overlay { display: none; position: absolute; right: 8px; bottom: 8px; max-width: 360px; max-height: 40vh; overflow: auto; background: rgba(13,17,23,0.82); border: 1px solid var(--border); border-radius: 6px; padding: 0.5rem 0.65rem; font-family: ui-monospace, monospace; font-size: 0.68rem; line-height: 1.35; color: var(--muted); z-index: 1000; pointer-events: none; }
    #ingest-overlay.visible { display: block; }
    #ingest-overlay h3 { margin: 0 0 0.35rem; font-size: 0.7rem; color: var(--text); font-weight: 600; }
    #ingest-overlay .group { margin-bottom: 0.45rem; }
    #ingest-overlay .group-title { color: var(--text); font-weight: 600; }
    #ingest-overlay .warn .group-title { color: #d29922; }
    #ingest-overlay .row { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  </style>
</head>
<body>
  <div class="layout">
    <aside id="sidebar">
      <h1>Gedung Peristiwa</h1>
      <p class="sub" id="region-subtitle">{{.ActiveRegion.Label}}</p>
      <h2>Region</h2>
      <div id="region-list">
        {{range .Regions}}
        <button type="button" class="region-btn{{if eq .ID $.ActiveRegion.ID}} active{{end}}" data-region="{{.ID}}" onclick="window.switchRegion('{{.ID}}')">{{.Label}}</button>
        {{end}}
      </div>
      <h2>Tenants</h2>
      <ul class="tenant-list" id="tenant-list">
        {{range .Feeds}}
        <li>{{.Region}} <span style="opacity:0.7">({{.Agency}})</span></li>
        {{end}}
      </ul>
      <h2>Stats</h2>
      <div class="stats" id="stats">
        <p>Vehicles: <span id="vehicle-count">0</span></p>
        <p>Updated: <span id="last-update">—</span></p>
        <p>Events: <span id="event-count">0</span></p>
      </div>
      <label class="debug-toggle">
        <input type="checkbox" id="debug-ingest"> Debug ingest
      </label>
      <p id="stream-status" class="status">Connecting…</p>
    </aside>
    <div id="map-wrap">
      <div id="map"></div>
      <div id="ingest-overlay" aria-hidden="true"></div>
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
      attribution: '&copy; OpenStreetMap',
      maxZoom: 19
    }).addTo(map);
    setTimeout(() => map.invalidateSize(), 100);

    const markers = {};
    const color = '#3498db';
    let currentAgencies = new Set({{mustJSON .ActiveRegion.Agencies}});
    let stream = null;
    let activeRegionId = activeRegion;
    let activeRegionLabel = activeLabel;

    const statusEl = document.getElementById('stream-status');
    const overlayEl = document.getElementById('ingest-overlay');
    const debugToggle = document.getElementById('debug-ingest');

    if (sessionStorage.getItem('debugIngest') === '1') {
      debugToggle.checked = true;
    }
    debugToggle.addEventListener('change', () => {
      sessionStorage.setItem('debugIngest', debugToggle.checked ? '1' : '0');
      overlayEl.classList.toggle('visible', debugToggle.checked);
      if (!debugToggle.checked) overlayEl.innerHTML = '';
    });

    window.updateVehicle = function(v, skipFilter) {
      const id = v.id;
      const latlng = [v.lat, v.lng];
      const popup = '<strong>' + v.agency + '</strong><br>Route: ' + (v.route || '—') +
        '<br>Speed: ' + (v.speed ? v.speed.toFixed(1) : '0') + ' km/h';
      if (markers[id]) {
        markers[id].setLatLng(latlng);
        markers[id].setPopupContent(popup);
      } else {
        markers[id] = L.circleMarker(latlng, {
          radius: 7, color: color, fillColor: color, fillOpacity: 0.9, weight: 2
        });
        markers[id].bindPopup(popup);
        markers[id]._agency = v.agency;
        if (currentAgencies.has(v.agency)) markers[id].addTo(map);
      }
      if (!skipFilter) window.applyAgencyFilter();
    };

    window.applyAgencyFilter = function() {
      Object.entries(markers).forEach(([id, m]) => {
        const show = currentAgencies.has(m._agency);
        if (show && !map.hasLayer(m)) m.addTo(map);
        else if (!show && map.hasLayer(m)) map.removeLayer(m);
      });
    };

    function clearStaleMarkers(agencies) {
      Object.entries(markers).forEach(([id, m]) => {
        if (!agencies.has(m._agency)) {
          if (map.hasLayer(m)) map.removeLayer(m);
          delete markers[id];
        }
      });
    }

    function applyVehiclesChunked(list) {
      const chunk = 40;
      let i = 0;
      function step() {
        const slice = list.slice(i, i + chunk);
        if (slice.length === 0) {
          window.applyAgencyFilter();
          statusEl.textContent = 'Live — ' + list.length + ' vehicles';
          statusEl.className = 'status ok';
          return;
        }
        slice.forEach(v => window.updateVehicle(v, true));
        i += chunk;
        requestAnimationFrame(step);
      }
      requestAnimationFrame(step);
    }

    function applyStats(signals) {
      if (signals.vehicleCount != null) document.getElementById('vehicle-count').textContent = signals.vehicleCount;
      if (signals.lastUpdate != null) document.getElementById('last-update').textContent = signals.lastUpdate;
      if (signals.eventCount != null) document.getElementById('event-count').textContent = signals.eventCount;
    }

    function renderIngest(data) {
      if (!debugToggle.checked) return;
      overlayEl.classList.add('visible');
      let html = '<h3>Ingest — ' + (data.activeLabel || '') + '</h3>';
      const polled = (data.polledAgencies || []).filter(Boolean);
      if (polled.length) {
        html += '<div class="row">polled: ' + polled.join(', ') + '</div>';
      }
      const records = data.records || [];
      records.forEach(row => {
        html += '<div class="row">' + row.agency + ' ' + row.vehicle + ' ' +
          row.lat.toFixed(2) + ',' + row.lng.toFixed(2) + ' ' + row.at + '</div>';
      });
      if (records.length === 0) html += '<div class="row">no ingest yet for this region</div>';
      overlayEl.innerHTML = html;
    }

    function connectStream() {
      if (stream) { stream.close(); stream = null; }
      stream = new EventSource('/api/vehicles/stream');

      stream.addEventListener('vehicles', e => {
        applyVehiclesChunked(JSON.parse(e.data));
      });

      stream.addEventListener('stats', e => applyStats(JSON.parse(e.data)));
      stream.addEventListener('ingest', e => renderIngest(JSON.parse(e.data)));

      stream.onopen = async () => {
        await syncRegionFromServer();
        statusEl.textContent = 'Connected, waiting for data…';
        statusEl.className = 'status ok';
      };
      stream.onerror = () => {
        if (!stream) return;
        if (stream.readyState === EventSource.OPEN) return;
        statusEl.textContent = 'Reconnecting…';
        statusEl.className = 'status';
      };
    }

    function setActiveRegionButton(id) {
      document.querySelectorAll('.region-btn').forEach(btn => {
        btn.classList.toggle('active', btn.getAttribute('data-region') === id);
      });
    }

    function updateTenantList(feeds) {
      const ul = document.getElementById('tenant-list');
      ul.innerHTML = feeds.map(f =>
        '<li>' + f.region + ' <span style="opacity:0.7">(' + f.agency + ')</span></li>'
      ).join('');
    }

    window.switchRegion = async function(id) {
      if (id === activeRegionId) return;
      statusEl.textContent = 'Switching region…';
      statusEl.className = 'status';
      try {
        const res = await fetch('/api/region', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id: id })
        });
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        const region = data.region;
        activeRegionId = region.id;
        activeRegionLabel = region.label;
        currentAgencies = new Set(region.agencies);
        document.getElementById('region-subtitle').textContent = region.label;
        setActiveRegionButton(region.id);
        updateTenantList(data.feeds || []);
        map.flyTo([region.center[0], region.center[1]], region.zoom, { duration: 1 });
        clearStaleMarkers(currentAgencies);
        connectStream();
      } catch (err) {
        statusEl.textContent = 'Region switch failed';
        statusEl.className = 'status err';
        console.error(err);
      }
    };

    async function syncRegionFromServer() {
      const res = await fetch('/api/region');
      if (!res.ok) return;
      const data = await res.json();
      const region = data.region;
      if (!region || !region.id) return;
      activeRegionId = region.id;
      activeRegionLabel = region.label;
      currentAgencies = new Set(region.agencies || []);
      document.getElementById('region-subtitle').textContent = region.label;
      setActiveRegionButton(region.id);
      if (data.feeds) updateTenantList(data.feeds);
    }

    setTimeout(async () => {
      try {
        statusEl.textContent = 'Connecting…';
        await syncRegionFromServer();
        connectStream();
        if (debugToggle.checked) overlayEl.classList.add('visible');
      } catch (err) {
        statusEl.textContent = 'Connection failed';
        statusEl.className = 'status err';
        console.error(err);
      }
    }, 200);
  </script>
</body>
</html>
`
