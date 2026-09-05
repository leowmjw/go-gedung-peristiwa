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

**Live UI contract (keep):** `Write` → `FlushAll` → `NotifyPoll` → in-memory `LivePositionsFor` → SSE. Replay is a separate ChangeFeed read path.

See **Future Ideas / Roadmap** below for remaining demo polish.

## Live-map freshness (DONE — read this before touching the map)

The in-memory projection accumulates **every vehicle ever seen** (hydration replays the
whole change feed on startup). Rendering all of it made the live map lie: Penang showed
**130** buses when the feed only reports ~16–20 at any instant. The fix is a **read-side**
freshness filter — the write path still persists everything for replay/analytics.

| Age since GTFS `timestamp` | Live map |
|---|---|
| ≤ 5 min (`LiveMapFreshWindow`) | normal blue marker |
| 5–30 min | **gray** marker, `stale: true` in JSON |
| > 30 min (`LiveMapVisibleWindow`) | **hidden** (still in IsleDB / D1) |

**Go oracles:** `LiveMapFreshWindow` / `LiveMapVisibleWindow` + `LivePositionsFor` /
`StatsFor(agencies, now)` in [`internal/demo/pipeline.go`](internal/demo/pipeline.go);
`liveViews` / `replayViews` in [`internal/web/demo/server.go`](internal/web/demo/server.go).
**Worker port:** `FRESH_MS` / `VISIBLE_MS` + `timestamp_ms >= ?` in `latest()` and `stats()`
(`workerTS` in `internal/codexsites/templates.go`).

**Rules:**
- `LatestPositions` / `LatestPositionsFor` are **unfiltered** — analytics/debug only. Never
  wire them back into the live map or the sidebar count.
- Replay must **never** set `stale` (staleness is a live-map concept) — use `replayViews`.
- The client **must evict markers missing from the latest payload** (`dropMissing` in
  `render.go`, `if(!seen.has(id)` in the Worker `app.js`). Without it, vehicles that age past
  30 min stay frozen on the map forever. Locked by `TestIndexHTMLEvictsMissingMarkers` and a
  `compiler_test.go` grep.

**Oracle tests:** `internal/demo/pipeline_fresh_test.go`,
`TestGetVehiclesStaleFlag` / `TestLiveViewsMarksStale` / `TestReplayViewsNeverStale`.

### Known gaps (next agent)

1. **`vehicleSeen` is unbounded.** The freshness filter hides ancient vehicles but never
   evicts them, so memory still grows with every unique vehicle id ever ingested. Startup
   hydration (`hydrateFromSnapshot` in `internal/demo/pipeline.go`) now reads a `Reader.BootstrapView`
   KV snapshot per agency instead of replaying the full `ChangeReader` from `Oldest` — a direct
   range scan rather than walking change-feed batches, and atomically bound to a resume cursor
   per the v0.7.0 IsleDB docs. It does **not** fix the underlying growth: position keys embed
   `timestamp_ns` (`gtfs.VehiclePosition.Key`), so every historical Put is still a distinct KV
   entry the snapshot walks. Add pruning (ticker or post-`Write`) for entries older than
   `LiveMapVisibleWindow`, and consider skipping over-window entries during hydration/scan.
   Same idea for the Worker: D1 `vehicle_positions` grows forever; a periodic
   `DELETE WHERE timestamp_ms < ?` (or Cron Trigger) would bound it.
2. **Staleness only re-evaluates on poll.** `pushSnapshot` stamps `now` per SSE push, and
   pushes are driven by `NotifyPoll`, so a vehicle crossing the 5- or 30-min line can be up to
   one poll interval (10–30 s) late to gray out / disappear. Acceptable today; if it ever
   matters, send `lastSeen` and let the client recompute on a timer.
3. **Popup text is imprecise.** Stale markers say “Last seen > 5 min ago” for anything in the
   5–30 min band. The payload already carries the timestamp — render “12 min ago” instead.
   Applies to both `render.go` and `templates.go` (`appJS`).
4. **Worker `/api/status` `records` are unfiltered.** The debug overlay lists the last 10
   `ingest_events` rows regardless of freshness. Fine for a debug tool; filter it if that list
   is ever promoted into the main UI.
5. **No composite index for the freshness scan.** `latest()` / `stats()` now filter
   `agency IN (...) AND timestamp_ms >= ?` but `idx_positions_region_agency` does not cover
   `timestamp_ms`. Row counts are tiny today; add
   `(agency, timestamp_ms)` to `schema.sql` if the projection grows.

## Future Ideas / Roadmap

### Write-through persistence (keep)
- **Ingest everything we care about** into IsleDB/MinIO on every poll, even if the map
  does not display it yet — data is available for analytics, exports, and future views
  without re-fetching GTFS history.
- **Live UI stays on the in-memory read model** (`vehicleSeen`, updated in `Write`, read via
  `LivePositionsFor`) plus `NotifyPoll` batch SSE — not per-mutation ChangeFeed to each
  browser client.
- Rationale: tail replay + per-key fan-out is wrong for live SSE; write-through + projection
  is correct CQRS (see **IsleDB Learnings**).
- The read model is **filtered by freshness** on the way out — see
  **Live-map freshness** above. Persist everything; display only what is current.

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

## IsleDB Learnings (v0.7.0)

Pinned `github.com/ankur-anand/isledb@v0.7.0`. v0.4 prefixes are incompatible; fresh data under `gedung-peristiwa` bucket, reader cache under `data/cache/gedung-peristiwa/`.

v0.5.0 → v0.5.2 is additive only: adds `Reader.BootstrapView` (snapshot+cursor bound to the same manifest boundary, for materializing state and resuming the change feed from an exact point), `Reader.BloomCacheStats`, `ReaderOptions.BloomCacheSize`, and `ErrCommitIndeterminate`.

v0.5.2 → v0.7.0 (adds `github.com/gofrs/flock` as a transitive dep) is also additive for the APIs we use: `Open`/`OpenBucket`, `DB.OpenWriter`/`OpenReader`/`OpenChangeReader`/`OpenMaintenance`, `DefaultWriterOptions`/`DefaultReaderOpenOptions`/`DefaultMaintenanceOptions`/`DefaultChangeFeedRetentionOptions`, and `ChangeFeedOptions{Payload: ChangeFeedFullValues}` are all unchanged. Change-feed GC (`change_feed_gc.go`, `ChangeFeedRetentionOptions`) already existed in v0.5.2 — we already used it; v0.7.0 hardens its deletion-plan writes (checksum-verified two-phase canonical/ready object paths) against concurrent-writer races. A maintenance scheduler/fault-injection layer and internal reader/compactor rewrites round out the diff — none required code changes. Verified by upgrading go.mod and running `go build ./...` + `go vet ./...` + `go test ./...`, all clean with zero source changes at the time of the bump.

### API (what we use)

- `isledb.Open` / `OpenBucket` with `DBOptions{Prefix, ChangeFeed: {Payload: ChangeFeedFullValues}}`
- One `Writer`, one long-lived `Reader`, one `Maintenance` per prefix (`OpenPrefixDB` in `internal/pipeline/db.go`)
- `Writer.Put(ctx, …)` / `Flush(ctx)` — visibility boundary; `WriterOptions.Flush.Interval` for background flush
- `OpenChangeReader` + `Read` until `CaughtUp()` — replay, verify
- `Reader.BootstrapView(ctx)` — atomic KV `Snapshot` + resume `Cursor`, used for startup hydration
- `Maintenance.Run` in-process for demo/simulate; `RunOnce` in tests/close
- **Removed in v0.5:** `OpenDB`, `OpenCompactor`, `TailingReader`, public `manifest/` package

### ChangeFeed is real — live UI still must not fan out per mutation

ChangeFeed writes ordered batches under `changes/`. We enable `ChangeFeedFullValues` on every database.

**Live map:** write-through IsleDB + in-memory `vehicleSeen` + `NotifyPoll` batch SSE (`event: vehicles`). Do **not** stream every `Change` to the browser.

**Startup hydration:** `NewPipeline` calls `Reader.BootstrapView` per agency and iterates the
returned `Snapshot` (a direct KV range scan) into `vehicleSeen`, rather than replaying
`ChangeReader` from `Bounds().Oldest`. `BootstrapView` binds the snapshot and resume cursor
atomically in one call — do not reconstruct that boundary by calling `Snapshot()` and
`ChangeReader.Bounds()` separately (a writer publishing between those two calls produces a
cursor newer than the snapshot and silently skips a committed change). We don't currently
resume the feed from the returned cursor; live updates come from `Write()` in-process, not a
replayed feed.

**Replay / verify:** walk `ChangeReader`; FinTech `Verify` counts feed changes ≥ unique keys.

### Change-feed retention (GC)

`MaintenanceOptions.ChangeFeedRetention` bounds how far back `ChangeReader`/`DrainChangeFeed`
can replay — this is the historical-replay depth, **not** the KV data itself (position keys
embed `timestamp_ns`, so KV state keeps every write regardless; see Known gaps below).
`internal/pipeline/db.go` sets this whenever `PrefixOpenConfig.Retention` is true (every
non-memory backend). `RetainFor` defaults to `StoreConfig.ChangeFeedRetainFor`, normalized by
`pipeline.NormalizeChangeFeedRetainFor`: zero/unset → 30 days
(`DefaultChangeFeedRetainFor`), clamped to a 1-year max (`MaxChangeFeedRetainFor`). Override via
the `CHANGEFEED_RETAIN_FOR` env var (Go duration string, e.g. `720h`) — see `.env.example`.
Pick this deliberately: it must be ≥ whatever range the `/replay` catalog advertises, or replay
requests for older windows will silently come back empty once GC reclaims that history.

### Rolling deploys / fencing

Writer and Maintenance ownership are fenced through the object store (isledb, not something we
built): "different processes cannot safely act as the same owner at the same time." This is
exactly the Kubernetes rolling-update case — `maxSurge: 1, maxUnavailable: 0` briefly runs the
new pod's process (which opens its own `Writer`/`Maintenance` on the same prefix at startup,
independent of whether it has passed a readiness probe yet) alongside the old pod still draining.
The moment the new pod opens, the old pod's writer is fenced. No corruption, ever — proven
against the real dependency in `internal/pipeline/fencing_test.go` (`TestRollingDeployFencing`):
whichever writer opens most recently becomes the sole owner immediately, with zero warm-up, and
this holds no matter how long a wall-clock delay separates the fencing writer from the one before
it (the test opens a *third* writer after an already-fenced second one to demonstrate this). So:
if the new pod's rollout is delayed, the moment it (or a retry/replacement of it) finally does
open a writer, it works correctly, exactly as if the delay hadn't happened — fencing is
manifest-commit-generation-based, not a lease with a TTL that could get stuck or need to expire.

**What is not true, and was wrong in an earlier version of this file and of
`cmd/demo/main.go`:** application code cannot reliably detect "I was fenced" via any exported
isledb error. `writer.go` deliberately excludes fence errors from ever becoming the exported
`isledb.ErrWriterFailed` (`terminalOnError && !isFenceError(err)` before recording it), and the
underlying `manifest.ErrFenced` sentinel lives in an unexported internal package. A fenced
writer's `Put`/`Flush` just returns a plain wrapped error — `errors.Is(err, isledb.ErrWriterFailed)`
and `errors.Is(err, isledb.ErrWriterClosed)` are both false for it. `fencing_test.go` asserts this
directly and will fail loudly if a future isledb version changes it.

Given that, `cmd/demo/main.go`'s `pollLoop` does **not** try to fast-exit specifically on
fencing — there is no reliable signal to trigger on, and treating *every* write failure as "must
be fenced, exit now" would kill the pod on an ordinary transient MinIO error too, which is worse
than doing nothing. A fenced old pod instead keeps polling GTFS and logging `write failed`/
`flush failed` every tick — noisy, but harmless — until Kubernetes' own SIGTERM (scaling down the
old ReplicaSet) arrives; it keeps serving reads from its last-known `vehicleSeen` state the whole
time, so there is no availability gap. `stopIfWriterDead` in `pollLoop` still checks
`ErrWriterFailed`/`ErrWriterClosed` as a genuine fast-exit for a *different* class of failure —
a writer that isledb has independently marked definitively terminal (e.g. a background flush that
fails for a reason other than fencing) — just not this one.

What we do fix, all in `internal/pipeline/db.go`:
- `WriterOptions.OnFlushError` / `MaintenanceOptions.OnError` log terminal writer/maintenance
  failures instead of the previous silent `_ = maintenance.Run(runCtx)` swallow — including
  fencing, since `Maintenance.Run` does return (rather than loop forever) once its fence is lost,
  even though we can't specifically label *why* it returned.
- `PrefixDB.Close` used to `return` on the *first* failed step (commonly `Writer.Flush` once
  fenced), skipping `Maintenance.Close`/`Writer.Close`/`Reader.Close`/`closeBucket` entirely —
  leaking every handle after it on exactly the fenced-shutdown path this section is about. It
  now runs every step regardless and returns the first error, matching `Pipeline.Close`'s
  already-correct per-agency loop. This is what actually matters for a delayed/eventual rollout:
  whenever SIGTERM does arrive (on time or late), shutdown still completes cleanly.

This is single-writer-per-prefix semantics tolerating a brief overlap, not true multi-writer
horizontal scaling — `replicas` should stay at 1 per prefix; readers (`OpenReaderDB`) scale
independently and are unaffected by fencing.

#### Edge case: a bad rollout that gets rolled back can strand the old pod fenced forever

A "bad" new pod's process can call `OpenWriter` (fencing the old, good pod) during startup
*before* it fails its own health checks — our `NewPipeline` opens the writer well before the HTTP
server would ever answer a readiness probe. Confirmed by web search against Kubernetes' and Argo
Rollouts' documented behavior: with `maxUnavailable: 0`, the old ReplicaSet is never scaled down
while the new one is pending, so a rollback (`kubectl rollout undo`) or an aborted Argo Rollout
just scales the *already-running* old ReplicaSet back to its existing count and scales the bad one
to zero — it does not restart the surviving old pod's process. If that old pod's writer was
already fenced by the bad pod before it got torn down, the old pod stays alive but permanently
unable to write, and nothing in the rollback path ever fixes it.

`isledb` has no TTL/lease-based recovery for this — verified in
`internal/pipeline/fencing_test.go` (`TestFencedWriterNeverSelfRecovers`): a fenced `Writer`'s
`fenced` field is a plain `atomic.Bool` with no timestamp, so hammering it with `Put`/`Flush` for a
full second (with no competing writer ever appearing) fails identically on every attempt. This is
not a shortcoming to route around — unlike a lease with a TTL (e.g. "break the lock if unrenewed
for 10s"), which needs a wait period *and* clock-skew reasoning between owners, isledb's
CAS-on-manifest-commit fencing recovers **instantly** the moment any process actually calls
`OpenWriter` again (`TestRollingDeployFencing` proves this holds no matter how long a delay
precedes that call) — there's no expiry to design or wait out. What's genuinely missing is
something calling `OpenWriter` again at all.

**Fix (implemented):** since nothing in the Kubernetes/Argo rollback path restarts this specific
pod, recovery has to come from our own liveness probe. There is no exported isledb signal for "my
writer is fenced" (see above), so the probe can't check that specifically — instead
`internal/demo/health.go`'s `WriteHealth` tracks "N consecutive `Write`/`FlushAll` failures from
`pollLoop`, of any cause" (`RecordSuccess`/`RecordFailure`, default threshold
`DefaultWriteHealthThreshold = 3`, overridable via `cmd/demo/main.go`'s `-write-health-threshold`
flag) and `GET /healthz` in `cmd/demo/main.go` (`healthzHandler`) reports `503` once that
threshold is reached. Point a Kubernetes liveness probe at it; kubelet then restarts the
container, and a fresh process reopens the writer and reclaims ownership immediately per
`TestRollingDeployFencing` — no wait, no backoff needed on the recovery side. A single failed
`Write`/`FlushAll` alone does not flip `/healthz` — only a *sustained* run does, so an ordinary
transient MinIO/Tigris blip that self-resolves on the next poll never triggers a restart. Unit
tests: `internal/demo/health_test.go`.

Example Kubernetes wiring (not committed as a manifest in this repo):
```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8081
  initialDelaySeconds: 10
  periodSeconds: 15
  failureThreshold: 1  # /healthz already debounces via -write-health-threshold; don't double it here
```

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
