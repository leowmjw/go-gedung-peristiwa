package demo

// indexHTML is the single-page transit map (Leaflet, no build step).
const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Gedung Peristiwa — KL Transit Live</title>
  <link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" />
  <script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
  <style>
    :root { --bg: #0d1117; --panel: #161b22; --text: #e6edf3; --muted: #8b949e; --border: #30363d; }
    * { box-sizing: border-box; }
    body { margin: 0; font-family: system-ui, sans-serif; background: var(--bg); color: var(--text); }
    .layout { display: flex; height: 100vh; }
    aside { width: 260px; background: var(--panel); border-right: 1px solid var(--border); padding: 1rem; overflow-y: auto; }
    aside h1 { font-size: 1rem; margin: 0 0 0.5rem; }
    aside h2 { font-size: 0.85rem; color: var(--muted); margin: 1rem 0 0.5rem; text-transform: uppercase; letter-spacing: 0.05em; }
    label { display: block; font-size: 0.85rem; margin: 0.25rem 0; cursor: pointer; }
    #map { flex: 1; min-height: 0; }
    .stats p { margin: 0.35rem 0; font-size: 0.9rem; }
    .stats span { font-weight: 600; }
    .sub { color: var(--muted); font-size: 0.8rem; margin-bottom: 1rem; }
    .status { font-size: 0.8rem; color: var(--muted); margin-top: 0.5rem; }
    .status.ok { color: #3fb950; }
    .status.err { color: #f85149; }
  </style>
</head>
<body>
  <div class="layout">
    <aside id="agency-panel">
      <h1>Gedung Peristiwa</h1>
      <p class="sub">Kuala Lumpur — RapidKL live</p>
      <h2>Agencies</h2>
      {{range .Feeds}}
      <label>
        <input type="checkbox" checked data-agency="{{.Agency}}" class="agency-filter" onchange="window.applyAgencyFilter()">
        {{.Region}}
      </label>
      {{end}}
      <h2>Stats</h2>
      <div class="stats" id="stats">
        <p>Vehicles: <span id="vehicle-count">0</span></p>
        <p>Updated: <span id="last-update">—</span></p>
        <p>Events: <span id="event-count">0</span></p>
      </div>
      <p id="stream-status" class="status">Connecting…</p>
    </aside>
    <div id="map"></div>
  </div>
  <script>
    const map = L.map('map', { preferCanvas: true }).setView([3.139, 101.687], 12);
    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      attribution: '&copy; OpenStreetMap',
      maxZoom: 19
    }).addTo(map);
    setTimeout(() => map.invalidateSize(), 100);

    const markers = {};
    const color = '#3498db';

    window.updateVehicle = function(v) {
      const id = v.id;
      const latlng = [v.lat, v.lng];
      const popup = '<strong>' + v.agency + '</strong><br>Route: ' + (v.route || '—') +
        '<br>Speed: ' + (v.speed ? v.speed.toFixed(1) : '0') + ' km/h';
      if (markers[id]) {
        markers[id].setLatLng(latlng);
        markers[id].setPopupContent(popup);
      } else {
        markers[id] = L.circleMarker(latlng, {
          radius: 7,
          color: color,
          fillColor: color,
          fillOpacity: 0.9,
          weight: 2
        }).addTo(map);
        markers[id].bindPopup(popup);
        markers[id]._agency = v.agency;
      }
      window.applyAgencyFilter();
    };

    window.applyAgencyFilter = function() {
      const enabled = new Set();
      document.querySelectorAll('.agency-filter:checked').forEach(el => {
        enabled.add(el.getAttribute('data-agency'));
      });
      Object.values(markers).forEach(m => {
        if (enabled.has(m._agency)) {
          if (!map.hasLayer(m)) m.addTo(map);
        } else if (map.hasLayer(m)) {
          map.removeLayer(m);
        }
      });
    };

    function applyStats(signals) {
      if (signals.vehicleCount != null) {
        document.getElementById('vehicle-count').textContent = signals.vehicleCount;
      }
      if (signals.lastUpdate != null) {
        document.getElementById('last-update').textContent = signals.lastUpdate;
      }
      if (signals.eventCount != null) {
        document.getElementById('event-count').textContent = signals.eventCount;
      }
    }

    const statusEl = document.getElementById('stream-status');
    const stream = new EventSource('/api/vehicles/stream');

    stream.addEventListener('vehicles', e => {
      const list = JSON.parse(e.data);
      let moved = 0;
      list.forEach(v => {
        const had = !!markers[v.id];
        window.updateVehicle(v);
        if (had) moved++;
      });
      statusEl.textContent = 'Live — ' + list.length + ' vehicles (' + moved + ' moved)';
      statusEl.className = 'status ok';
    });

    stream.addEventListener('stats', e => {
      applyStats(JSON.parse(e.data));
    });

    stream.onopen = () => {
      statusEl.textContent = 'Connected, loading…';
      statusEl.className = 'status ok';
    };

    stream.onerror = () => {
      statusEl.textContent = 'Stream disconnected — retrying…';
      statusEl.className = 'status err';
    };
  </script>
</body>
</html>
`
