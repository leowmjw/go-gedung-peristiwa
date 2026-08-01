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
- Use overmind to start MinIO + simulate (`mise run dev` — FinTech MVP UI + MinIO)
- Use `mise run demo` for transit map (`Procfile.demo`, port `DEMO_HTTP_ADDR=:8081`)
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
- [x] Procfile + overmind — `mise run dev` → `overmind start` (`Procfile`: MinIO + `scripts/dev.sh` → `minio-setup` + air on `DEV_HTTP_ADDR`, default `:8080`). PRD standalone MinIO path.

### Phase 3: Malaysia Transit Demo (DONE)
- [x] `cmd/demo` — GTFS poller, IsleDB on MinIO, Leaflet map (`mise run demo`, `Procfile.demo`, `:8081`)
- [x] `internal/gtfs/` — all **15** feeds; **8 region buckets** in `regions.go`: Klang Valley (default), National (KTMB), Penang, **East Coast** (Kuantan + Kelantan + Terengganu), Johor, Sarawak, **Northern** (Perlis, Kedah, Perak), Central (N. Sembilan, Melaka)
- [x] `internal/demo/` — per-agency IsleDB writers; in-memory projection + `NotifyPoll` batch SSE (not per-key tail)
- [x] `internal/web/demo/` — SSE (`vehicles`, `stats`, `ingest`); session cookie `demo_sid`; region persisted in `data/demo-sessions.json`
- [x] `PollCoordinator` — poll union of session regions; per-region freshness (no duplicate GTFS for same region / many viewers)
- [x] Authoritative region switcher; debug ingest filtered per session region
- [ ] Per-agency toggles within active region (optional UX)
- [ ] Datastar v2 migration (still plain JS + EventSource)

### Deferred (post-MVP)
- Temporal workflows
- Poll all 15 feeds every cycle regardless of viewers (analytics mode; today polls union of session regions only)
- github.com/tigrisdata/storage-go (Tigris-specific APIs beyond S3)

### Next agent: Historical replay (priority demo feature)

**Goal:** Playback stored vehicle positions from IsleDB — scrubber + speed (1×, 10×, 60×). **Not** live SSE tail.

**Do not** attach live map SSE to `TailingReader.Tail` (see IsleDB Learnings — per-key fan-out broke the UI).

**Suggested approach:**
1. New route/page or mode, e.g. `GET /replay` + `GET /api/replay/stream` (or WebSocket), separate from `/api/vehicles/stream`.
2. One background reader per replay session: `ScanLatest` / `CatchUp` on agency prefixes, or time-filtered scan; dedupe by `agency:vehicle_id`; batch patches to client (same `vehicles` JSON shape as live).
3. Reuse `internal/demo/pipeline.go` — `TailUpdates` / agency `scan` are starting points; `cmd/isledb-debug` for tail visibility experiments.
4. Keys are `{agency}:{vehicle_id}:{timestamp_ns}` — replay must pick latest per vehicle per playback time or walk timeline.
5. Tests: memory backend, no GTFS; unit test replay dedupe + ordering.

**Live UI contract (keep):** `Write` → `FlushAll` → `NotifyPoll` → in-memory `LatestPositionsFor` → SSE. Replay is a separate read path.

See **Future Ideas / Roadmap** below for remaining demo polish.

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

**Done:** center + region buckets, authoritative switcher, session-scoped region + SSE filter.

**Still optional:**
- Sub-checkboxes per feed/agency within the active region.
- Datastar v2 instead of hand-written SSE/Leaflet JS.
- Multi-region view (“add region” mode) — explicitly not in v1.

**National bucket:** KTMB under top-level **National** (not Klang Valley).

### Historical replay
- Moved to **Next agent: Historical replay** above (implementation brief).

### Other (from deferred)
- Temporal workflows for long-running ingest / replay jobs
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
