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
- Cloudflare Worker compiler: `mise run sites:compile` / `sites:test` — see **`internal/codexsites/AGENTS.md`** (Go demo vs D1+R2 deploy target; wrangler pin; internal Access)
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

**Status:** Implemented — `GET /replay`, `GET /api/replay/catalog`, `GET /api/replay/stream` (batch `event: vehicles`, separate from live SSE). Catalog summarizes IsleDB **change-feed** history per agency; playback walks `ChangeReader` from Oldest with optional RFC3339 `from`/`to` filters.

**Do not** attach live map SSE to per-mutation ChangeFeed fan-out (same UX problem as old TailingReader).

**Live UI contract (keep):** `Write` → `FlushAll` → `NotifyPoll` → in-memory `LatestPositionsFor` → SSE. Replay is a separate ChangeFeed read path.

See **Future Ideas / Roadmap** below for remaining demo polish.

## Future Ideas / Roadmap

### Write-through persistence (keep)
- **Ingest everything we care about** into IsleDB/MinIO on every poll, even if the map
  does not display it yet — data is available for analytics, exports, and future views
  without re-fetching GTFS history.
- **Live UI stays on the in-memory read model** (`LatestPositions`, updated in `Write`)
  plus `NotifyPoll` batch SSE — not per-mutation ChangeFeed to each browser client.
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

## IsleDB Learnings (v0.5.0)

Pinned `github.com/ankur-anand/isledb@v0.5.0`. v0.4 prefixes are incompatible; fresh data under `gedung-peristiwa` bucket, reader cache under `data/cache/gedung-peristiwa/`.

### v0.5 API (what we use)

- `isledb.Open` / `OpenBucket` with `DBOptions{Prefix, ChangeFeed: {Payload: ChangeFeedFullValues}}`
- One `Writer`, one long-lived `Reader`, one `Maintenance` per prefix (`OpenPrefixDB` in `internal/pipeline/db.go`)
- `Writer.Put(ctx, …)` / `Flush(ctx)` — visibility boundary; `WriterOptions.Flush.Interval` for background flush
- `OpenChangeReader` + `Read` until `CaughtUp()` — replay, verify, startup hydration
- `Maintenance.Run` in-process for demo/simulate; `RunOnce` in tests/close
- **Removed in v0.5:** `OpenDB`, `OpenCompactor`, `TailingReader`, public `manifest/` package

### ChangeFeed is real — live UI still must not fan out per mutation

ChangeFeed writes ordered batches under `changes/`. We enable `ChangeFeedFullValues` on every database.

**Live map:** write-through IsleDB + in-memory `vehicleSeen` + `NotifyPoll` batch SSE (`event: vehicles`). Do **not** stream every `Change` to the browser.

**Startup hydration:** `NewPipeline` drains each agency feed from `Bounds().Oldest` into `vehicleSeen`.

**Replay / verify:** walk `ChangeReader`; FinTech `Verify` counts feed changes ≥ unique keys.

### Why we abandoned TailingReader for live SSE (historical note)

v0.4 `TailingReader.Tail` replayed all keys on connect and emitted per-key events (~164/poll), flooding EventSource. v0.5 ChangeFeed has the same fan-out risk if wired directly to live SSE. Batch projection remains correct CQRS.

**Demo fix (current):** after each poll `Write` + `FlushAll` → `NotifyPoll()` → SSE reads `LatestPositionsFor`.

Code: `internal/demo/pipeline.go`, `internal/web/demo/server.go`, `cmd/demo/main.go`.

### Debug harness: ChangeReader vs object-store visibility

Use `cmd/isledb-debug` — minimal writer + `ChangeReader` on a throwaway prefix.

```bash
mise run dev:isledb-debug              # memory baseline
mise run dev:isledb-debug-minio        # local MinIO
mise run dev:isledb-debug-tigris       # Tigris creds
mise run dev:isledb-debug-compare      # memory + MinIO back-to-back
```

| Experiment | What it tests | Healthy signal |
|---|---|---|
| `visibility` | flush batch-2 → poll ChangeReader after delays | `keys_seen=5` by ≤500ms memory; MinIO may need longer |
| `incremental` | drain to head → write+flush → poll feed | `feed_events=5`, `first_new_ms` < 2s |
| `replay` | count feed from Oldest after seed flush | `replay_events` > 0 |

**Unit tests (no external deps):**
```bash
go test ./internal/isledbdebug/... -v
go test ./internal/demo/ -run TestPipelineWriteScanChangeFeed -v
```

Code: `internal/isledbdebug/harness.go`, `internal/pipeline/db.go`, `cmd/isledb-debug/main.go`.
