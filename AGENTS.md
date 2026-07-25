# AGENTS

## Language

- Use latest Go v1.26.x and its full capabilities
- Always prefer stdlib if available and it make sense

## Orchestration

- Use Temporal + Go SDK to handle the workflows
- Use Temporal testsuite as much as possibel to have widest test coverage
- Use advanced techique like registerdelayedcallback to simulate long running system
- Ensure use Temporal Worker version to ensure multiple workflow versions can co-exists

## Runtime

- Whole system should be fully testable standalone with Go binary
- Use modern Go capabilities (when needed): generics, structured log, built-in http
routing, testing/synctest
- Use techniques like first class anonymous function as method replacement, synctest
 to ensure all things are deterministic

## Testing

- All new cases should be at least 80% coverage
- Unit tests and inetgration tests MUST be comepleted without needing to spin up any
 external dependencies
- Only Full End-to-End should need to stand up Temporal Test Server

## Data Location

- Any persistence store locally can store in the data folder; create if not available
- If scratch pad or data; can use the tmp folder 

## Tools

- Use mise to run tasks, set env variables, automate
- Tools available: ripgrep, fzf, air, goreleaser, watchexec
- Use overmind to start temporal-cli + air

## Specification (MVP)

- Follow PRD.md for high level business objective
- Follow TECHSPEC.md for suggested details but it MUST NOT override what stated here
- Ask if anything unsure or contradictory

## Specification (Advanced) 
- Finally CI/CD will use End-to-End Tests
- Implement this ONLY after Unit/Integration tests are passing

## Implementation Status & Learnings

### Phase 1: Documentation & Setup (DONE)
- PRD.md written — covers vision, problem statement, solution, MVP scope, success criteria
- TECHSPEC.md written — architecture, component design, event generation, testing strategy, tradeoffs
- mise.toml created — env vars, doctor task, test task, simulate task, minio-setup task
- Procfile created — overmind manages MinIO

### Phase 2: Implementation (TODO)
- [ ] Add IsleDB + Tigris SDK dependencies to go.mod
- [ ] Implement internal/model/ (Event types)
- [ ] Implement internal/eventgen/ (multi-tenant event generator with traffic patterns)
- [ ] Implement internal/pipeline/ (IsleDB writer/reader/tailer wrappers)
- [ ] Implement cmd/simulate/ (main simulation binary)
- [ ] Write unit tests with blobstore.NewMemory()
- [ ] Write integration tests (full pipeline)
- [ ] E2E test with MinIO via overmind

