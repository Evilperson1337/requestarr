# Ticket01 verification — 2026-11-05

## Scope and revisions

- Ticket: [01 — The local administrator can sign in](issues/01-local-administrator-can-sign-in.md).
- Inputs: [spec](spec.md), [D040](../../docs/DECISIONS.md:327), [agent guardrails](../../AGENTS.md), [assistant instructions](../../CLAUDE.md), [coding standards](../../CODING_STANDARDS.md), and [local tracker conventions](../../docs/agents/issue-tracker.md).
- Integration worktree: `.claude/worktrees/local-admin-integration`, branch `feat/local-admin`, starting at `0e21d3e93f51917a561da2f0d4c2dd1d7e6aa7bd`.
- Protected root: clean `feat/unraid-deployment`, `a5dbdb4af7bc43e6e5541d79a1a715e3701695fd`. No root edits, branch changes, hook changes, worktree removals, or unrelated-worktree changes.
- New isolated effort worktree: `.claude/worktrees/local-admin-ticket01-verification`, branch `fix/local-admin-ticket01-verification`. It is preserved for the red/green evidence.
- Existing implementation: `0e21d3e93f51917a561da2f0d4c2dd1d7e6aa7bd`.
- Effort fix `fc14075a1ba1903d69b8ed92133122f20b77ac38` → integration `614a4fb73ee3e05e315ddc9efe94c32ba4aeed67`.
- Effort follow-up `b1d6a820f3efc1adbdd63c0d65c5597238742f18` → integration `ca6010cbfb65b723951c7ff3f2b386bb5320f6b4`.
- All code checks below were executed against `ca6010cbfb65b723951c7ff3f2b386bb5320f6b4`. The resolution commit only records ticket status and this evidence; no later ticket is implemented.

## Findings and deterministic fixes

### Standards

Initial review identified non-atomic password-change auditing, secret-bearing assertion diagnostics, and an undocumented extension of the existing authentication routing exception. Password changes now commit their audit with the credential update and session revocations; failure diagnostics omit credentials; the narrow auth bootstrap exception is documented, without changing general route policy. Final review: zero blocking findings.

### Spec

Initial review identified session creation from a stale password after password-change revocation and overlapping guesses passing the throttle before failure recording. A row-locked credential check now serializes local session insertion with password replacement; one in-flight verification is reserved per account/address, with bounded memory and deferred release. The follow-up regression also proves rejected stale sign-ins record failure rather than success; successful sign-in audit and session insertion share one transaction. Final review: zero blocking findings.

Seven recorded red/green cases cover the original session boundary, the post-verification boundary, the PostgreSQL credential guard, concurrent account/address attempts, handler audit failure, PostgreSQL audit rollback, and stale-sign-in audit classification. Each recorded red exit is 1 and green exit is 0. The concurrency tests synchronize with channels and five-second context deadlines, not sleeps. Database failure injection uses only a test-private database.

## Acceptance coverage pointers

| Acceptance behavior | Evidence |
| --- | --- |
| Append-only credential/audit schema, built-in admin, fresh install and upgrade | [migration](../../db/migrations/000035_local_admin.up.sql), [down migration](../../db/migrations/000035_local_admin.down.sql), [PostgreSQL tests](../../internal/store/postgres/local_admin_integration_test.go) |
| PBKDF2-SHA256, 600,000 iterations, salts, stored parameters, rehash checks | [hashing tests](../../internal/auth/password/password_test.go) |
| Generated or supplied initial password; generated password appears once only | [bootstrap tests](../../internal/auth/bootstrap_test.go), [real database flow](../../internal/store/postgres/local_admin_integration_test.go:249) |
| Opaque sessions, CSRF/Origin, wrong/unknown/disabled credentials, uniform hashing work | [handler tests](../../internal/auth/local_test.go) |
| Forced change and unauthorized/viewer/requester/admin paths | [router tests](../../internal/httpapi/local_auth_test.go), [role matrix](../../internal/httpapi/role_matrix_test.go) |
| Delay growth/cap, no lockout, per-account/address overlap protection | [handler cases](../../internal/auth/local_test.go), [concurrency regressions](../../internal/auth/local_concurrency_test.go) |
| Password length, current password, other-session revocation, transactional audit rollback | [handler tests](../../internal/auth/local_test.go), [PostgreSQL regressions](../../internal/store/postgres/local_verification_integration_test.go) |
| Local form, forced change, desktop/phone accessibility, unchanged OIDC action | [Playwright cases](../../web/tests/local-signin.spec.ts) |
| Route/OpenAPI agreement | [contract tests](../../internal/httpapi/openapi_contract_test.go:60) |

## Executed gates

Every listed command exited 0. No required gate was skipped.

| Command | Result / private log under integration worktree |
| --- | --- |
| `make fmt` | No tracked formatting diff; `tmp/ticket01-verification-20261105/verified-make-fmt.log` |
| `make test` | Pass; `tmp/ticket01-verification-20261105/verified-make-test.log` |
| `make vet` | Pass; `tmp/ticket01-verification-20261105/verified-make-vet.log` |
| `make web-check` | App and test typechecks pass; `tmp/ticket01-verification-20261105/verified-make-web-check.log` |
| `scripts/check.sh` | Deployment/export checks, PostgreSQL, typecheck, 250 Playwright tests pass (58.5s); `tmp/ticket01-verification-20261105/check-verified.log` |
| `go test -count=1 -timeout=120s -json ./...` | 539 top-level tests, 1,662 test/subtest pass events, zero test failures/skips; `tmp/ticket01-verification-20261105/unit-verified.json.log` |
| `go test -race -count=1 -timeout=120s ./internal/auth/... ./internal/httpapi/...` | Pass; `tmp/ticket01-verification-20261105/race-verified.log` |
| `go test -tags integration -count=1 -timeout=180s -json ./internal/store/postgres/...` | 124 top-level tests, 161 test/subtest pass events, zero failures/skips; `tmp/ticket01-verification-20261105/integration-verified.json.log` |
| `make build` | TypeScript/Vite production UI plus static Go binary build pass; `tmp/ticket01-verification-20261105/build-verified.log` |

The ordinary Go JSON run reports 34 tested packages and five package-level no-test-file entries. Those are not skipped tests or skipped gates. Integration ran separately with the build tag and an explicit dedicated test database.

The post-resolution documentation check is recorded separately in `tmp/ticket01-verification-20261105/check-resolution.log` and the same gate ledger; it is not a replacement for the code-revision checks above.

## Environment and log safety

- Host: Go 1.27.3, Node 26.13.0, npm 12.2.1, Docker 29.9.2 / Compose 5.7.0. CI/Docker's Node-major consistency guard passes; this local run is not a claim of execution under CI's Node 22.
- Existing PostgreSQL services/ports and the existing browser-test server were left alone. Verification used effort-owned PostgreSQL 17 container `requestarr-ticket01-verify-20261105`, an allocated loopback-only host port, database `requestarr_test`, and Playwright port 4197. The full suite's shared database was reset only inside this dedicated container; fresh-store tests create separate databases there.
- PostgreSQL readiness used the database readiness probe. Playwright managed its own Vite server. The fixture browser suite emitted seven nonfatal Vite proxy refusals for requests lacking API mocks; all 250 assertions still passed. No live application backend, real OIDC service, or production reverse proxy was started for those browser tests.
- Raw output was redirected to ignored private logs; generated database credentials were kept in a mode-0600 environment file under the private log directory and never printed or committed. Do not publish raw logs or source that file with shell tracing enabled.
- The gate ledger is [gates.tsv](../../tmp/ticket01-verification-20261105/gates.tsv). Primary script context is [check-verified.log](../../tmp/ticket01-verification-20261105/check-verified.log). Effort red/green context is [EVIDENCE.txt](../../../local-admin-ticket01-verification/tmp/ticket01-fixes/EVIDENCE.txt). These local artifacts are intentionally not committed.

## Concise repository architecture

- **Module / executable:** [Go module](../../go.mod) `github.com/evilperson1337/requestarr`, Go 1.27.1 target; [command entry point](../../cmd/requestarr/main.go). A modular monolith serves the HTTP API and embedded UI and runs background jobs.
- **Boundaries:** [domain](../../internal/domain/request.go) owns core rules; [requests](../../internal/requests/service.go), inventory, notifications, issues, jobs, and checklist implement use cases. [app](../../internal/app/app.go) wires config, stores, adapters, HTTP and background work. Metadata and library providers stay behind narrow interfaces; libraries are authoritative for availability, with no acquisition orchestration.
- **HTTP / authentication:** Go standard-library HTTP routing, central access/CSRF policy and [OpenAPI contract](../../api/openapi.yaml); opaque hashed-at-rest sessions, OIDC via go-oidc/oauth2, and the single local-admin password path. [Architecture](../../docs/ARCHITECTURE.md) describes data flow and targeted reconciliation.
- **Persistence / configuration:** PostgreSQL via pgx and explicit SQL; embedded append-only migrations applied with golang-migrate. [Environment configuration](../../internal/config/config.go) supports a database URL or separate database fields; selected runtime settings are stored in PostgreSQL and stored integration secrets are encrypted with the operator's master key.
- **Frontend:** [React 19 / TypeScript / Vite](../../web/package.json), native routing/state conventions, an API client and page/components; production assets are embedded by the Go web UI package.
- **Tests / build / CI:** Go tests with deterministic fakes and HTTP test servers; tagged PostgreSQL integration and shared adapter contracts; Playwright Chromium and axe accessibility with mocked API fixtures. [Make targets](../../Makefile), [full check script](../../scripts/check.sh), [CI](../../.github/workflows/ci.yml), multi-stage Docker, and GoReleaser build/check/release the UI plus Go executable.

## Remaining coverage limits

No ticket01 blocker remains. A full deployed-browser/OIDC/HTTPS end-to-end test was not run or counted as passed. Browser tests use mocks; the local-auth persistence/handler flow is exercised against real PostgreSQL. Complete no-OIDC startup/runtime configuration belongs to ticket03; claim-flow removal/reset belongs to ticket02. Those and tickets04–06 remain untouched. This is ticket01 verification, not a claim that the entire D040 feature is complete.
