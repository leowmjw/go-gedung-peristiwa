# AGENTS

## Language

- Use latest Go v1.26.x and its full capabilities
- Always prefer stdlib if available and it make sense

## Orchestration

### MVP
- Single-process simulation binary (`cmd/simulate`) — no Temporal
- Use `testing/synctest` for deterministic time-dependent tests
- Use overmind to start MinIO + simulation together

### Advanced (post-MVP)
- Use Temporal + Go SDK to handle long-running workflows
- Use Temporal testsuite and `RegisterDelayedCallback` for workflow timing
- Ensure Temporal Worker versioning for multiple workflow versions
- Use overmind to start temporal-cli + air

## Runtime

- Whole system should be fully testable standalone with Go binary
- Use modern Go capabilities (when needed): generics, structured log, built-in http
routing, testing/synctest
- Use techniques like first class anonymous function as method replacement, synctest
 to ensure all things are deterministic

## Testing

- All new cases should be at least 80% coverage
- Unit tests and integration tests MUST be completed without needing to spin up any
 external dependencies
- E2E MinIO: overmind + `mise run test-e2e` (or `//go:build e2e` tag)
- E2E Tigris: `mise run simulate-tigris` with credentials (manual/CI secret)
- Temporal Test Server: Advanced phase only

## Data Location

- Any persistence store locally can store in the data folder; create if not available
- If scratch pad or data; can use the tmp folder

## Tools

- Use mise to run tasks, set env variables, automate
- **All env vars load from `.env`** via `mise.toml` → `[env] _.file = ".env"` (copy from `.env.example`)
- Tools available: ripgrep, fzf, air, goreleaser, watchexec
- Use overmind to start MinIO + simulate (`mise run dev`)
- Use `mise run demo` for KL transit map (`Procfile.demo`, port `DEMO_HTTP_ADDR=:8081`)
- IsleDB tail debugging: `mise run dev:isledb-debug` (see **IsleDB Learnings** below)

## Specification (MVP)

- Follow PRD.md for high level business objective
- Follow TECHSPEC.md for suggested details but it MUST NOT override what stated here
- Ask if anything unsure or contradictory
- MVP validates MinIO locally, then the same pipeline against Tigris (S3 API) with read-back verification

## Specification (Advanced)
- Finally CI/CD will use End-to-End Tests
- Implement this ONLY after Unit/Integration tests are passing

## Implementation Status & Learnings

### Phase 1: Documentation & Setup (DONE)
- PRD.md written — covers vision, problem statement, solution, MVP scope, success criteria
- TECHSPEC.md written — architecture, component design, event generation, testing strategy, tradeoffs
- mise.toml created — env vars, doctor task, test task, simulate task, minio-setup task
- Procfile created — overmind manages MinIO

### Phase 2: Implementation (DONE)
- [x] Pin isledb + uuid in go.mod
- [x] Implement internal/model/ (Event types, UUID v7 key format)
- [x] Implement internal/eventgen/ (multi-tenant event generator with traffic patterns)
- [x] Implement internal/pipeline/ (IsleDB writer/reader/tailer wrappers; MinIO + Tigris backends)
- [x] Implement cmd/simulate/ (main simulation binary, `--backend minio|tigris|memory`)
- [x] Write unit tests with blobstore.NewMemory()
- [x] Write integration tests (full pipeline)
- [x] E2E test tag + mise tasks (`test-e2e`, `simulate-tigris`)
- [ ] Procfile + overmind E2E (manual: `mise run dev` with MinIO running)

### Phase 3: KL Transit Demo (DONE)
- [x] `cmd/demo` — GTFS poller (KL feeds), IsleDB on MinIO, Leaflet map UI
- [x] `internal/gtfs/` — 15 feeds defined; demo uses `gtfs.KLFeeds()` (RapidKL + MRT Feeder)
- [x] `internal/demo/` — per-agency IsleDB writers; in-memory `LatestPositions()` for live UI
- [x] `internal/web/demo/` — plain JSON SSE (`event: vehicles`, `event: stats`); no Datastar CDN
- [x] `Procfile.demo` + `mise run demo` (separate from dev `Procfile`)
- [x] Authoritative region switcher — center+zoom buckets (default Klang Valley, National for KTMB); SSE filtered by active region; debug ingest overlay

### Deferred (post-MVP)
- Temporal workflows
- Full Malaysia map (all 15 feeds; demo is KL-only for now)
- github.com/tigrisdata/storage-go (Tigris-specific APIs beyond S3)

See **Future Ideas / Roadmap** below for demo UI, viewport filtering, and historical replay.

## Future Ideas / Roadmap

### Write-through persistence (keep)
- **Ingest everything we care about** into IsleDB/MinIO on every poll, even if the map
  does not display it yet — data is available for analytics, exports, and future views
  without re-fetching GTFS history.
- **Live UI stays on the in-memory read model** (`LatestPositions`, updated in `Write`)
  plus `NotifyPoll` batch SSE — not `TailingReader` per browser client.
- Rationale: tail replay + per-key fan-out is wrong for live SSE; write-through + projection
  is correct CQRS (see **IsleDB Learnings**).

### Live map — viewport & tenant filtering

**Decision: center + region buckets** (not bbox). Server maps active region → center/zoom
+ tenant set; coarse over-fetch within the bucket is fine.

**Default:** Klang Valley selected on load, with all tenants under that bucket checked
(current demo: RapidKL Bus + MRT Feeder). Other regions deselected.

**Region switcher UX (authoritative):**
- Top-level region control (tabs or list): **Klang Valley** (default), **National**, Johor,
  Sarawak, Penang, … — same level; user picks exactly one active region.
- On switch: **fly map** to region center/zoom → **deselect previous** tenants → **auto-select
  all tenants** in the new bucket. One mental model: “I’m looking at this region.”
- Manual map pan/zoom does **not** auto-change region (explicit switch only — keeps client
  and SSE state simple for Datastar v2).
- Optional sub-checkboxes per feed/agency within the active region for power users later.

**National bucket:** cross-region tenants (e.g. **KTMB** rail) live under a top-level
**National** region — not folded into Klang Valley or duplicated per state. Selecting
National flies to a peninsula-wide (or country) view and streams only national-scope
agencies. FinTech multi-tenant analog: tenants that span regions get their own National
bucket rather than being attached to a local region.

**Why this over bbox:** matches how users think (state/valley/national), trivial server
filter (`region_id` → agency list), no per-frame bounds sync.
**Trade-off:** no multi-region view until we add an explicit “add region” mode.

- **Client stays simple** — target [Datastar v2](https://data-star.dev/) patches over a
  mostly server-rendered map; region + tenant selection driven from server or simple
  `data-on-click` handlers; avoid SPA-style per-marker subscriptions.
- **Full tenant selection** — all GTFS feeds / FinTech tenants available via region buckets,
  not KL-only demo feeds.

### Historical replay (separate feature — not live UI)
- Use IsleDB `CatchUp` / `Tail` (or `ScanLatest` + time-range filter) to **replay stored
  positions** for “watch how it flowed” playback — e.g. scrubber + speed multiplier
  (1×, 10×, 60×).
- **Do not** wire live SSE to per-key tail; replay is a dedicated mode (separate handler
  or page), one background reader, dedupe by `agency:vehicle_id`, batch patches to the map.
- `internal/demo/pipeline.go` `TailUpdates` remains test/harness code until this feature
  is built.

### Other (from deferred)
- Temporal workflows for long-running ingest / replay jobs
- Full Malaysia map (all 15 GTFS feeds)
- Tigris-specific APIs beyond S3

## IsleDB Learnings (v0.4.2)

### ChangeFeed does not exist
Pinned `github.com/ankur-anand/isledb@v0.4.2` has **no `ChangeFeed` field** on `WriterOptions`.
`DEMO.md` / `TECHSPEC.md` references to `opts.ChangeFeed.Enabled` are aspirational.
Use `FlushInterval` on the writer + `TailingReader` (`CatchUp` / `Tail`) for ordered replay.

### TailingReader is fine for batch verification, flaky for live UI fan-out
FinTech simulation (`internal/pipeline/tenant.go` → `TailCatchUp`) works: one-shot
catch-up after flush, assert key count in tests.

**Why live SSE + `TailingReader.Tail` failed in the KL demo** (first render OK, map
stuck thereafter):

| Factor | Effect |
|---|---|
| Historical replay | Each SSE client opens new tailers per agency; `Tail` replays all existing keys on connect — duplicates the initial `ScanLatest` snapshot |
| Per-key fan-out | One SSE `vehicle` event + `stats` per tailed key (~164/poll) overwhelms the browser EventSource queue |
| Object-store visibility | Writer flush → manifest/SST visible on MinIO has latency; 500ms `PollInterval` tail loop can miss or delay updates |
| Same-process writer + tailer | Demo writes and tails the same prefix in one process; refresh timing is harder to reason about than read-only tailers |
| Channel backpressure | `TailUpdates` uses a buffered chan (256); tail replay can fill it while the HTTP handler is still draining stale keys |

**Symptom:** stats may tick (polling works), markers render once, then do not move on
subsequent 30s GTFS polls.

**Demo fix (current):** do **not** tail for the browser. After each poll
`Write` + `FlushAll`, call `Pipeline.NotifyPoll()`; SSE handlers push
`Pipeline.LatestPositions()` (in-memory latest per `agency:vehicle_id`) as a
batch `event: vehicles`. IsleDB remains the durable write path; UI reads memory.

Code: `internal/demo/pipeline.go` (`NotifyPoll`, `SubscribePolls`, `LatestPositions`),
`internal/web/demo/server.go` (`handleVehicleStream`), `cmd/demo/main.go` (calls
`NotifyPoll` after poll).

### How to reproduce / test tailing separately (another session)

**Unit test (passes, memory backend):**
```bash
go test ./internal/demo/ -run TestPipelineWriteScanTail -v
```
Writes positions, flushes, asserts `TailUpdates` receives at least one event.

**Simulate the UI failure (MinIO, tail-driven SSE):**
1. Temporarily revert `handleVehicleStream` to use `TailUpdates` instead of
   `SubscribePolls` (or checkout pre-notify commit).
2. `mise run demo` → open http://localhost:8081
3. Confirm first marker render; wait 30–60s for next GTFS poll
4. Observe: `Updated` time may change but markers stay static; Network tab shows
   flood of `event: vehicle` lines, not a clean `event: vehicles` refresh

**Compare with working path:**
```bash
curl -sN --max-time 65 http://localhost:8081/api/vehicles/stream | rg 'event: vehicles'
```
Expect **≥2** `event: vehicles` lines across one poll interval (initial + post-poll).
Tail-only path sends hundreds of `event: vehicle` lines and rarely updates the map.

**Key format note:** `{agency}:{vehicle_id}:{timestamp_ns}` — each poll creates new
keys; tail emits every key, but UI dedupes by `agency:vehicle_id`. Compaction does
not help the live tail fan-out problem.

### Debug harness: isolate IsleDB vs MinIO vs Tigris (no demo required)

Use `cmd/isledb-debug` — minimal writer + `TailingReader` on a throwaway prefix.
No GTFS, HTTP, or map. Answers: **is tail flakiness IsleDB core or S3 visibility?**

```bash
mise run dev:isledb-debug              # memory baseline (~instant visibility)
mise run dev:isledb-debug-minio        # local MinIO (needs MinIO up)
mise run dev:isledb-debug-tigris       # Tigris (needs creds in .env)
mise run dev:isledb-debug-compare      # memory + MinIO back-to-back
```

Single experiment flags:
```bash
go run ./cmd/isledb-debug/ --backend minio --experiment visibility
go run ./cmd/isledb-debug/ --backend tigris --experiment incremental --wait 15s
```

| Experiment | What it tests | Healthy signal | Suggests bug in |
|---|---|---|---|
| `visibility` | Write batch-2, flush, CatchUp after 0–5000ms delays | `keys_seen=5` by ≤500ms on memory; MinIO may need longer | Object-store manifest/SST visibility if memory OK but MinIO/Tigris slow |
| `incremental` | `Tail` running, then write+flush new batch | `tail_events=5`, `timeout=false`, `first_new_ms` < 2s | IsleDB `Tail` loop or same-process refresh if **all** backends timeout |
| `replay` | `Tail` with no new writes for 2s | `replay_events_in_2000ms` > 0 | Explains demo SSE flood (historical replay), not a storage bug |

**How to read compare output:**

1. **Memory passes, MinIO/Tigris fail `incremental` or high `visibility` delay** → S3-compatible
   layer (eventual consistency, list/head latency). Mitigation: poll+broadcast UI (current demo),
   or separate tailer process, or longer tail `PollInterval` + reader `Refresh`.
2. **All backends fail `incremental`** → IsleDB `TailingReader.Tail` behaviour or harness bug;
   file issue upstream with repro from `internal/isledbdebug/`.
3. **All pass but demo SSE still stuck** → UI/integration issue (per-key SSE flood, EventSource),
   not IsleDB storage — see replay experiment counts.

**Unit tests (no external deps):**
```bash
go test ./internal/isledbdebug/... -v
```

**Agent next steps if MinIO-specific:** sweep `--wait`, writer `FlushInterval`, tail
`PollInterval`, and `reader.Refresh()` before `CatchUp` in harness; bisect minimum delay.
If Tigris same as MinIO → not MinIO-specific. If only MinIO → check path-style endpoint,
`AWS_S3_USE_PATH_STYLE`, bucket CAS timing.

Code: `internal/isledbdebug/harness.go`, `cmd/isledb-debug/main.go`.
