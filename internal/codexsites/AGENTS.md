# Codex Sites (Cloudflare Worker compiler)

Isolated **read model** of the Malaysia transit demo for Cloudflare. It does **not** import IsleDB, MinIO, or `cmd/demo`. Re-run the compiler whenever Go region/feed config or the live/replay contract changes.

## Link to the main site

| | Go demo (`cmd/demo`, `:8081`) | This package → `codex-sites/` (gitignored) |
|---|---|---|
| Source of truth | `internal/gtfs`, `internal/demo`, `internal/web/demo` | Generated only. Edit `compiler.go` + `templates.go`. |
| Durable store | IsleDB on MinIO/Tigris | D1 (sessions, projection, catalog) + R2 (immutable GTFS batches) |
| Live UI | `NotifyPoll` → in-memory latest → SSE `event: vehicles` | Short-lived `GET /api/vehicles` + `/api/status` (no live EventSource) |
| GTFS cadence | `PollCoordinator`, session 10/20/30s, idle TTL 5m | Browser `POST /api/poll`; D1 `region_poll_state` skip if fresh; pause when tab hidden |
| Replay | IsleDB snapshots / manifest log | R2 objects indexed by D1; `SELECT DISTINCT r2_key` so one object = one frame |
| Generate / run | `mise run demo` | `mise run sites:compile` then `mise run sites:test` (`wrangler` **4.118.0**, `:8787`) |

Keep the **browser JSON contract** aligned: `/api/regions`, `/api/region`, `/api/poll-interval`, `/api/vehicles`, `/api/status`, `/api/replay/catalog`, `/api/replay/stream` (SSE for replay only). Go extra: live SSE. Worker extra: `/api/poll`, `/api/ingest`.

`gtfs.AllRegions()` / `AllFeeds()` are compiled into `public/config.js` and `worker.ts`. Do not hand-edit `codex-sites/`.

## Architecture decisions

- **CQRS stays:** Worker writes R2 + D1 projection; the map reads D1. Do not attach live UI to per-mutation ChangeFeed / per-key SSE (see repo `AGENTS.md` IsleDB learnings).
- **Fetches for live, SSE for replay:** long-lived live EventSource on Workers repeats the KL demo flood. Replay SSE is a single session stream of distinct R2 frames.
- **No `strings.Replace` patches:** bake Worker behaviour into `templates.go` (`regionAgencies`, D1 batches of 90, `knownRegion` 400, poll skip).
- **Wrangler pin = workerd max date:** `mise.toml` `wrangler = "4.118.0"` only accepts `compatibility_date` **≤ 2026-08-06**. A newer date (e.g. 2026-08-16) fails: *newest date supported by this server binary is 2026-08-06*. Bump wrangler **and** the date together, then re-verify `mise run sites:test`.
- **Quoted replay ids:** unknown / `"klang-valley"` → HTTP 400 (Go `mustJSON` bug). Worker uses `SITE_CONFIG` JSON, not HTML-escaped strings.
- **`INGEST_TOKEN`:** Worker secret for `POST /api/ingest`. Local `sites:test` may omit it; production must set it (`wrangler secret put`).

## Learnings

- D1 `batch()` has a practical statement cap; chunk **90**.
- Snapshot rows are per-agency; replay must **DISTINCT `r2_key`** or the same R2 batch plays once per agency.
- Default poll is **10s** (not 30). Options **10 / 20 / 30** only. Auto-poll must skip inside that window (`skipped: true`); `?force=1` bypasses.
- `regionByID` must not silently fall back for catalog/stream/poll query params.
- Tests: `go test ./internal/codexsites/` (no Wrangler). Local run needs compile + `wrangler d1 execute --local` + `wrangler dev --local --persist-to codex-sites/.wrangler/state`.

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
