# Codex Sites (Cloudflare Worker bundle)

Isolated **read model** of the Malaysia transit demo for Cloudflare. It does **not** import IsleDB, MinIO, or `cmd/demo`.

## Three layers (what the compiler does vs what you port by hand)

| Layer | Source | When to change |
|---|---|---|
| **Compiled (data only)** | `gtfs.AllRegions()` / `AllFeeds()` → `public/config.js` + `SITE_DATA` in `worker.ts` | Go region/feed lists changed → `mise run sites:compile` |
| **Ported (hand-written)** | [`templates.go`](templates.go) — poller fairness, D1/R2, live UI, replay | Go **behavior** changed → edit template + update this map + extend `compiler_test.go` |
| **Runtime-specific** | Worker-only: `POST /api/poll`, `POST /api/ingest`. Go-only: live SSE | Document in behavior map below |

`Generate()` in [`compiler.go`](compiler.go) only JSON-encodes the catalog and copies template literals. It does **not** read `internal/demo`, `internal/gtfs/poller.go`, or `PollCoordinator`. A green `sites:compile` does **not** mean poller policy still matches Go.

Do not hand-edit gitignored `codex-sites/` as the source of truth — edit `templates.go`, then compile.

## Link to the main site

| | Go demo (`cmd/demo`, `:8081`) | This package → `codex-sites/` (gitignored) |
|---|---|---|
| Source of truth | `internal/gtfs`, `internal/demo`, `internal/web/demo` | Catalog from Go; behavior in `templates.go` |
| Durable store | IsleDB on MinIO/Tigris | D1 (sessions, projection, catalog) + R2 (immutable GTFS batches) |
| Live UI | `NotifyPoll` → in-memory latest → SSE `event: vehicles` | Short-lived `GET /api/vehicles` + `/api/status` (no live EventSource) |
| GTFS cadence | `PollCoordinator`, one region/tick, `PollSequential`, 429 backoff, 10/20/30s | Browser `POST /api/poll`; one region; feeds **serial**; D1 claim-before-fetch; skip if fresh; pause when tab hidden |
| Replay | IsleDB snapshots / manifest log | R2 objects indexed by D1; `SELECT DISTINCT r2_key` so one object = one frame |
| Generate / run | `mise run demo` | `mise run sites:compile` then `mise run sites:test` (`wrangler` **4.118.0**, `:8787`) |

Keep the **browser JSON contract** aligned: `/api/regions`, `/api/region`, `/api/poll-interval`, `/api/vehicles`, `/api/status`, `/api/replay/catalog`, `/api/replay/stream` (SSE for replay only). Go extra: live SSE. Worker extra: `/api/poll`, `/api/ingest`.

## Behavior map (codespec)

Re-read these Go symbols when syncing. If they changed, the Worker template may be stale.

### Upstream GTFS (data.gov.my) — MUST match Go demo

| Invariant | Go oracle | Worker port (`workerTS` in `templates.go`) |
|---|---|---|
| One region per poll | [`cmd/demo/main.go`](../cmd/demo/main.go) `pollLoop` → `FeedsForScheduledPoll` / `FeedsForRegionSwitch` | `POST /api/poll` for session region only |
| Feeds inside region are **serial** (max 1 in-flight to data.gov.my) | [`internal/gtfs/poller.go`](../gtfs/poller.go) `PollSequential` (demo path). **Not** `PollAll`. | `pollFeedsSequential` + `fetchFeed`; never `Promise.all` over `feed.url` |
| Feed order | [`internal/gtfs/feeds.go`](../gtfs/feeds.go) `AllFeeds()` filtered by region agencies | `gtfsFeedsFor` — same `FEEDS` array order |
| HTTP 429 retry | `pollFeed`: `maxPollAttempts` (6), backoff initial 1s doubling to a real 30s cap, honors `Retry-After` if sent | `fetchFeed` + `backoffMs`/`retryAfterMs`, `MAX_POLL_ATTEMPTS` (6) |
| Claim before fetch | Go: `MarkPolled` after success (single-process ticker) | D1 `claimRegionPoll` **before** first GET (Worker substitute for mutex) |

Oracle tests: `internal/gtfs/poller_extra_test.go` — `TestPollSequentialDoesNotOverlap`, `TestPollRateLimitRetry`.

### Cadence constants — MUST match

| Invariant | Go oracle | Worker port |
|---|---|---|
| Default 10s, options 10/20/30 | [`internal/demo/poll.go`](../demo/poll.go) `DefaultPollSeconds`, `AllowedPollSeconds` | `DEFAULT_POLL`, `ALLOWED_POLL` in template |
| Skip if region polled inside interval | `PollCoordinator.isStale` / `region_poll_state` | `claimRegionPoll` gap = `pollSeconds*1000`; `skipped: true` |
| Upstream fetch never faster than data.gov.my's refresh cadence, even if a session picked 10s/20s | `PollCoordinator.intervalLocked` floors on `gtfs.UpstreamRefreshInterval` (30s) | `claimRegionPoll` gap = `max(pollSeconds*1000, UPSTREAM_REFRESH_MS)` |
| Force refresh | `?force=1` on Worker | `claimRegionPoll` gap = 2s when `force` (bypasses the 30s floor deliberately) |

Oracle tests: `internal/demo/poll_test.go`.

### Live-map freshness — MUST match

| Invariant | Go oracle | Worker port |
|---|---|---|
| Fresh ≤ 5 min, visible ≤ 30 min | [`internal/demo/pipeline.go`](../demo/pipeline.go) `LiveMapFreshWindow`, `LiveMapVisibleWindow` | `FRESH_MS`, `VISIBLE_MS` |
| Hide vehicles past the visible window | `LivePositionsFor` / `StatsFor(agencies, now)` | `timestamp_ms >= ?` in `latest()` **and** `stats()` |
| Gray out 5–30 min vehicles | `liveViews` sets `Stale` | `stale:(now()-v.timestamp_ms)>FRESH_MS` |
| Replay is never stale | `replayViews` (no `Stale`) | replay reads R2 batches directly, no `stale` field |
| Client evicts markers missing from the payload | `dropMissing` in `internal/web/demo/render.go` | `if(!seen.has(id)` in `apply()` |

Oracle tests: `internal/demo/pipeline_fresh_test.go`, `internal/web/demo/server_internal_test.go`
(`TestLiveViewsMarksStale`, `TestReplayViewsNeverStale`, `TestIndexHTMLEvictsMissingMarkers`).

Do **not** drop the marker-eviction step: the live payload omits aged-out vehicles, so without
it they stay frozen on the map forever. Do **not** point the map at an unfiltered projection
read (`LatestPositionsFor` in Go; `SELECT ... FROM vehicle_positions` with no cutoff in D1).

### Allowed divergences (do not “fix” back to Go)

- Browser `POST /api/poll` per tab instead of server `PollTickInterval` ticker.
- No Klang Valley / Penang priority queue across regions (`PollCoordinator.nextDueRegion`).
- Two tabs on two regions may poll both concurrently; Go serializes across regions.
- Live map uses short `GET`s, not `EventSource` (IsleDB learnings — see repo `AGENTS.md`).

### Forbidden in `templates.go`

- `Promise.all(feeds.map` (or any concurrent fetch of `feed.url`)
- `EventSource('/api/vehicles/stream')` in `app.js`
- `/api/vehicles/stream` route in Worker (live SSE removed; replay SSE only)
- Porting `PollAll` as the demo poller path
- `latest()` / `stats()` without a `timestamp_ms` cutoff (stale vehicles would render as live)
- An `apply()` that adds markers without removing ids absent from the payload

## Agent sync checklist

Run this when a Go demo feature lands or before deploying the Worker:

1. **Read Go oracles** listed in the behavior map (symbols + tests). If changed, Worker is suspect even if compile is green.
2. **Classify the change:** catalog data | poller policy | browser JSON contract | Worker-only.
3. **Catalog only** → `Generate()` is enough (`mise run sites:compile`).
4. **Policy / UI** → edit `templates.go`, update this behavior map, add/adjust greps in [`compiler_test.go`](compiler_test.go).
5. **Lock:** `go test ./internal/codexsites/` (no Wrangler required). Compile bundle after tests pass.
6. **Smoke:** `mise run sites:test` when Wrangler bindings matter.

## Architecture decisions

- **CQRS stays:** Worker writes R2 + D1 projection; the map reads D1. Do not attach live UI to per-mutation ChangeFeed / per-key SSE (see repo `AGENTS.md` IsleDB learnings).
- **Fetches for live, SSE for replay:** long-lived live EventSource on Workers repeats the KL demo flood. Replay SSE is a single session stream of distinct R2 frames.
- **Templates are source of truth for Worker logic:** bake behaviour into `templates.go` (`regionAgencies`, D1 batches of 90, `knownRegion` 400, sequential poll).
- **Wrangler pin = workerd max date:** `mise.toml` `wrangler = "4.118.0"` only accepts `compatibility_date` **≤ 2026-08-06**. Bump wrangler **and** the date together, then re-verify `mise run sites:test`.
- **Quoted replay ids:** unknown / `"klang-valley"` → HTTP 400. Worker uses `SITE_CONFIG` JSON, not HTML-escaped strings.
- **`INGEST_TOKEN`:** Worker secret for `POST /api/ingest`. Local `sites:test` may omit it; production must set it (`wrangler secret put`).

## Learnings

- D1 `batch()` has a practical statement cap; chunk **90**.
- Snapshot rows are per-agency; replay must **DISTINCT `r2_key`** or the same R2 batch plays once per agency.
- Default poll is **10s** (not 30). Options **10 / 20 / 30** only. Auto-poll must skip inside that window (`skipped: true`); `?force=1` bypasses with a 2s claim gap.
- `regionByID` must not silently fall back for catalog/stream/poll query params.
- Parallel `Promise.all` on region feeds caused 429s; demo uses `PollSequential`.
- The projection accumulates every vehicle ever ingested, so an unfiltered `/api/vehicles`
  overstates reality (Penang showed 130 buses for a ~16–20 vehicle feed). Filter on
  `timestamp_ms` and evict missing markers client-side. `vehicle_positions` still grows
  forever — bounding it with a scheduled `DELETE` is an open task (repo `AGENTS.md`).

## Cursor + Cloudflare (internal app)

Install `/add-plugin cloudflare` (Skills + MCP). Prefer live Cloudflare docs (`developers.cloudflare.com/workers/llms.txt`, `…/d1/llms.txt`, `…/r2/llms.txt`) over training data. Load **wrangler**, **workers-best-practices**, **cloudflare-one** before deploy. `@` the generated `codex-sites/wrangler.jsonc` so bindings stay in context.

**Internal by default (do this on first production deploy):**

1. Cloudflare **Access on the Worker** (2026-08-14): policy attaches to the Worker, so `workers.dev`, custom domains, routes, and **preview URLs** stay behind company login. Prefer **account-wide Access** so every current/future Worker is private; do not add a public bypass for this app.
2. Who can sign in: Cloudflare account membership or company email domain (refine in Zero Trust if needed).
3. In Worker code, identity is `ctx.access.getIdentity()` (email, name, groups) — no JWT parsing. Gate `/api/ingest` on `INGEST_TOKEN` **in addition** to Access (machines are not browser SSO).
4. Local Access simulation (when implementing prod): `wrangler.jsonc` `access.dev` identity; omit it to test unauthenticated 401. Do **not** commit a real `aud` or production identity.
5. Secrets: `wrangler secret put INGEST_TOKEN` (or Secrets Store). Never commit `.dev.vars` with real tokens. D1 `database_id` in generated `wrangler.jsonc` is a placeholder until `wrangler d1 create`.
6. Cursor may deploy via terminal Wrangler or Cloudflare API MCP after OAuth. Scope OAuth tightly; do not paste API tokens into chat. Use `wrangler tail` / observability MCP after deploy, not production log dumps in transcripts.

## Future: `mise` production deploy

Not implemented. When added, a single internal-app task should:

1. `sites:compile`
2. Ensure remote D1 + R2 exist; apply `schema.sql` with migrations (do not wipe prod)
3. `wrangler secret put INGEST_TOKEN` if missing
4. Confirm Access is on the Worker (and account-default private)
5. `wrangler deploy --config codex-sites/wrangler.jsonc` (mise-pinned wrangler only)

Keep local `sites:test` unchanged. Production is an **internal** Access-protected Worker, not a public GTFS proxy.
