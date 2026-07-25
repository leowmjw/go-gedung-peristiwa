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

### Deferred (post-MVP)
- Temporal workflows
- DEMO.md (GTFS + Datastar map)
- github.com/tigrisdata/storage-go (Tigris-specific APIs beyond S3)
