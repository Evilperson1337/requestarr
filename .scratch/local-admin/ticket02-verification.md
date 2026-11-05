# Ticket02 verification and merger handoff — 2026-11-05

## Scope and isolation

- Ticket: [02 — Reset the password and remove the claim flow](issues/02-reset-the-password-and-remove-the-claim-flow.md).
- Inputs read: [spec](spec.md), [D040](../../docs/DECISIONS.md), [AGENTS](../../AGENTS.md), [CLAUDE](../../CLAUDE.md), [coding standards](../../CODING_STANDARDS.md), [glossary](../../GLOSSARY.md), [ticket01 verification](ticket01-verification.md), and local tracker conventions.
- Worker: `.claude/worktrees/agent-a0fcf7ab479c477ed`, branch `feat/local-admin-ticket02`. Confirmed clean before reuse; created a new branch from integration rather than resetting the abandoned branch. The old branch remains preserved.
- Integration base/latest: `feat/local-admin`, `1bbd46172e3b6a8ec707f4fca8958fdb1b1757fc`.
- Implementation commit: `534cdd73883958d454ee5b149a4184aa8ae6b46e`. All final code checks below ran on this committed revision, after the latest-integration merge attempt. A documentation-only evidence/resolution commit follows it.
- Executed `git merge --no-edit feat/local-admin` in the worker: exit 0, **Already up to date**. Confirmed integration is an ancestor with `git merge-base --is-ancestor feat/local-admin HEAD` (exit 0). No integration merge was performed in the other direction.
- Protected root remains clean on `feat/unraid-deployment`, `a5dbdb4af7bc43e6e5541d79a1a715e3701695fd`. No root file/branch/hook edits, destructive resets, unrelated-worktree edits/removals, or PR creation.
- The ticket01 verification worktree and all other worktrees remain preserved.

## Implementation and acceptance evidence

| Acceptance | Evidence |
| --- | --- |
| Shell-gated recovery through the shipped binary | [command](../../cmd/requestarr/main.go:95), [entrypoint/PostgreSQL test](../../internal/store/postgres/reset_admin_entrypoint_integration_test.go:22) |
| Random password printed once only after commit, no hash, no OIDC/session configuration needed | Entry point test runs a built binary with only database configuration; successful stdout is exactly one valid password line, stderr empty, and the printed value actually signs in. Unknown commands and missing database configuration fail before startup/implicit connections. |
| Forced change and all local sessions ended | The real account starts with a previously chosen password (must-change cleared). After reset both existing sessions and the old password fail; the printed password signs in with must-change true. An unrelated OIDC viewer's session and role survive. |
| Atomic state/audit, actionable failures | [PostgreSQL transaction](../../internal/store/postgres/local_auth.go:106). A test-private audit constraint forces failure: no password is printed, credential/must-change stay unchanged, and old sessions remain valid. Absent account fails clearly without creation. Disabled state is preserved, not silently re-enabled. |
| Claim flow removed while historic tables remain | Claim command, endpoint, response field, public setup status, startup hint, claim UI, token scripts and their obsolete tests removed. No migration was modified or added; historic schema/migration tests remain. The application no longer writes claim completion during local account creation. |
| Existing-admin-only promotion, no OIDC auto-admin | [role matrix](../../internal/httpapi/role_matrix_test.go:261) now explicitly promotes a person to admin, with unauthenticated/viewer/requester denials and admin success, plus CSRF/Origin coverage. Real PostgreSQL OIDC sign-in remains viewer. Existing default-role and persistent role tests remain in the full suites. |
| HTTP contract | [route/OpenAPI absence check and current-user role contract](../../internal/httpapi/admin_recovery_test.go:12), existing bidirectional OpenAPI route-table check. Current-user JSON has only user, csrfToken and mustChangePassword; no raw OIDC issuer/subject, and no-store remains. |
| UI no longer depends on removed status | [browser regression](../../web/tests/admin-recovery.spec.ts:4) makes obsolete setup requests return 404, proves none is made and the local sign-in is reachable, and runs axe. Existing local first-sign-in, password-change and checklist tests pass. Removed stale setup-status fixtures across browser suites. |
| Deployment-script surface | [stack-script test](../../scripts/stack-http-port-test.sh) verifies local-admin instructions and URL/config precedence using real Compose resolution without starting a deployment. Make no longer issues or rotates bootstrap tokens or automatically dumps a password. |
| Required first-run/operator docs | [First run](../../docs/FIRST_RUN.md), [Unraid](../../docs/UNRAID.md), [OIDC](../../docs/OIDC_AUTH.md), [manual testing](../../docs/MANUAL_TESTING.md), README and related active guides now document local sign-in and shell recovery. Accepted historic decisions/planning snapshots remain historical. |

## Confirmed TDD seams and red/green record

The user explicitly confirmed all four seams before the first test: binary + disposable PostgreSQL; HTTP router/contracts/roles; browser/API fixtures; deployment-script checks. Tests exercise public boundaries. SQL in the entrypoint test is confined to test-private audit failure injection, not normal behavior observation; outcomes are observed through credential/session APIs and sign-in.

| Vertical slice | Red log / exit | Green log / exit |
| --- | --- | --- |
| Recovery through the binary | `tmp/ticket02/reset-red.log` / 1 | `tmp/ticket02/reset-green.log` / 0 |
| Remove public claim-era routes and contract | `tmp/ticket02/routes-red.log` / 1 | `tmp/ticket02/routes-green.log` / 0 |
| Sign-in without retired setup status | `tmp/ticket02/browser-red.log` / 1 | `tmp/ticket02/browser-green.log` / 0 (18 focused browser tests) |
| Replace token-era stack guidance | `tmp/ticket02/deploy-red.log` / 1 | `tmp/ticket02/deploy-green.log` / 0 |
| Reject unknown commands before server startup | `tmp/ticket02/command-red.log` / 1 | `tmp/ticket02/command-green.log` / 0 |
| Require explicit database configuration | `tmp/ticket02/config-red.log` / 1 | `tmp/ticket02/config-green.log` / 0 |

The ledger includes two intermediate unsuccessful green attempts: a command dispatch edit had not persisted (corrected before the first successful reset run), and an existing matrix fake still expected the old demotion payload (aligned with the new explicit promotion case). Neither was counted as a passing gate. Additional rollback, unrelated-session and current-user contract coverage passed before final validation.

## Final executed validation

All commands below exited 0. No required gate was skipped. Paths are private ignored artifacts relative to this worker, not publishable raw logs.

| Command | Result | Log |
| --- | --- | --- |
| `make fmt` | Pass; clean tracked tree after formatting | `tmp/ticket02/final-fmt.log` |
| `make test` | Pass | `tmp/ticket02/final-test.log` |
| `make vet` | Pass | `tmp/ticket02/final-vet.log` |
| `make web-check` | App and browser-test TypeScript checks pass | `tmp/ticket02/final-web-check.log` |
| `bash scripts/stack-http-port-test.sh` | Pass, including real Compose config resolution | `tmp/ticket02/final-stack-http.log` |
| `bash scripts/check.sh` | All Go/deployment/export/PostgreSQL/frontend gates pass; **248 Playwright tests passed in 58.4s** | `tmp/ticket02/final-check.log` |
| `go test -count=1 -timeout=120s -json ./...` | **524 top-level tests**, 1,613 pass events including subtests, **0 failures, 0 skips** | `tmp/ticket02/final-unit-json.log` |
| `go test -race -count=1 -timeout=120s ./internal/auth/... ./internal/httpapi/...` | Pass | `tmp/ticket02/final-race.log` |
| `make build` | Production TypeScript/Vite assets and static Go executable build pass | `tmp/ticket02/final-build.log` |
| `go test -tags integration -count=1 -timeout=180s -json ./internal/store/postgres/...` | **121 top-level tests**, 155 pass events including subtests, **0 failures, 0 skips** | `tmp/ticket02/final-integration-json.log` |
| `git diff --check` | Pass | Rechecked before commit |
| Tracked obsolete-runtime reference audit | No claim/token route/command/response/status references in command, application, auth, HTTP, browser, script or OpenAPI code | `git grep` audit, excluding preserved historical schema/decisions/specs |

The ordinary unit suite reports package-level no-test-file entries; those are not skipped tests. PostgreSQL tests ran separately with the integration build tag. An earlier complete check also passed 248 browser tests (59.3s); the table above is the final committed-code run.

## Environment, credentials and resource ownership

- Linux; Go 1.27.3, Node 26.13.0, npm 12.2.1, Docker 29.9.2, Compose 5.7.0. Node CI/Docker consistency check passes; local Node is not a claim of execution under CI's Node 22.
- Effort-owned PostgreSQL 17 container: `requestarr-ticket02-20261105`, allocated loopback-only port, dedicated `requestarr_test` database. The shared test database was recreated only in this owned container before each complete integration run. Fresh-store entrypoint tests create and clean up separate test-private databases there. Existing PostgreSQL services were untouched.
- Playwright port 4298; its Vite server was managed by Playwright. No existing browser server was reused or stopped.
- Raw validation output redirected under mode-0700 `tmp/ticket02/`; generated database credentials stored only in ignored mode-0600 environment files. No credentials in command output or commits. A staged-diff scan confirmed the generated database credential was absent before committing.
- Gate ledger: `tmp/ticket02/gates.tsv`. Changed-file manifest: `tmp/ticket02/changed-files.tsv`. Do not publish raw logs or source private environment files under shell tracing.
- Keep this worker and the ticket01 verification worktree until the effort's final cleanup. The dedicated database container/private environment remain available for merger verification; cleanup must target only the effort-owned resources.

## Remaining scope and merger instructions

- This is **ticket02 only**, not completion of all D040 work. Ticket03/runtime OIDC configuration, Authentication Settings/creation/disable controls, the final sign-in-options design, and later deployment overrides remain separate tickets. Server no-OIDC startup was not changed or claimed here; the recovery command itself works without OIDC configuration.
- Browser coverage is fixture-backed, not a live OIDC/HTTPS/Unraid deployment acceptance test. No new live-service claim is made.
- Recovery intentionally preserves a disabled account's state; it never creates a local administrator on upgrade. Historic tables remain unused for claiming, not dropped or rewritten.
- Separate merger: use branch `feat/local-admin-ticket02` in `.claude/worktrees/agent-a0fcf7ab479c477ed`; inspect the tip and this evidence, then merge into `feat/local-admin` separately. The worker has already incorporated the latest integration tip. Do not merge or modify the protected root branch. No PR was created.
