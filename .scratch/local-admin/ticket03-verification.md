# Ticket03 verification — 2026-11-05

## Scope, ownership and revisions

- Ticket: [03 — OIDC configured at runtime](issues/03-oidc-configured-at-runtime.md); inputs: [spec](spec.md), [D040](../../docs/DECISIONS.md), [guardrails](../../AGENTS.md), [assistant instructions](../../CLAUDE.md), [standards](../../CODING_STANDARDS.md), [glossary](../../GLOSSARY.md), and prior [ticket01](ticket01-verification.md)/[ticket02](ticket02-verification.md) verification.
- Reused clean abandoned worktree: `.claude/worktrees/agent-a884313af677de3a3`. It was at `0e21d3e`; a new per-ticket branch `feat/local-admin-ticket03` was created and fast-forwarded to the requested integration tip before any implementation.
- Integration base and latest incorporated revision: `1e5a13d38ed045159b3151877e1d3cdf59a9cd21`, branch `feat/local-admin`, worktree `.claude/worktrees/local-admin-integration`.
- Implementation commit: `00feceaf85347ba525715cca2bdf9ce06afcffb9`. All final code validation below ran on this revision. The resolution/handoff commit changes only ticket/evidence documents.
- After committing implementation, `git merge --no-edit feat/local-admin` in the worker completed successfully, reporting “Already up to date”; ancestry was checked. The integration branch/worktree was never merged into or edited.
- Protected root remained clean on `feat/unraid-deployment`, `a5dbdb4af7bc43e6e5541d79a1a715e3701695fd`. No branch, hook, root file, other worker or shared-service modifications.
- TDD skill loaded and followed. The user explicitly confirmed four seams before tests: pure state; app/runtime service; HTTP/OpenAPI; PostgreSQL settings/secrets. The worker was marked claimed before implementation; resolved only after every required gate passed.

## Implemented behavior and file map

- [Pure state](../../internal/authsettings/state.go): all three OIDC environment values win as an indivisible unit; five states; missing/ignored/differing variable names only. Table covers all eight environment combinations with/without saved settings, including equal saved values still ignored.
- [Runtime service](../../internal/authsettings/service.go): serialized writes, atomically published provider/issuer snapshots, write-only secret handling, master-key reporting, discovery-on-enable/save, disable without provider access or decryption, discard, idempotent identical saves, and immutable previous snapshots. Failed Test, discovery, enable, encryption or persistence never replaces working runtime/saved state.
- [App assembly](../../internal/app/authentication.go) and [startup](../../internal/app/app.go): optional OIDC startup, complete environment compatibility, incomplete configuration and unavailable discovery leave local recovery reachable. Production public-origin and issuer URL validation is retained. [Config tests](../../internal/config/oidc_optional_test.go) cover absent/partial OIDC.
- [OIDC adapter](../../internal/auth/oidc/provider.go) and [bounded transport](../../internal/auth/oidc/http.go): context cancellation, ten-second client timeout, explicit user agent, JSON-only responses capped at 1 MiB, no redirects, HTTPS endpoints and development-only loopback HTTP. Safe stage-specific errors never quote upstream bodies/credentials. A signed discovery/token/JWKS fixture verifies PKCE, client authentication, claims, nonce rejection, and exchange after the discovery context is canceled.
- [Sign-in handler](../../internal/auth/handler.go): binds the original provider and identity issuer to each browser-bound five-minute transaction. Disable/reconfigure does not change in-flight callbacks. Pending snapshots expire and are capped at 4096; restart requires beginning that sign-in again. New sign-ins receive a safe 503 while OIDC is off.
- [HTTP API](../../internal/httpapi/authentication.go): admin-only state/save/test/enable-disable/discard through the route table. The unauthenticated options response contains exactly enabled/name, is non-cacheable, and makes no outbound call. Full mutation role/CSRF/Origin matrix plus read-role and [schema/contract tests](../../internal/httpapi/authentication_test.go). [Generic secret routes](../../internal/httpapi/secrets.go) cannot change the OIDC secret independently; OIDC status is owned by Authentication rather than exposing an unusable generic edit control.
- [PostgreSQL OIDC persistence](../../internal/store/postgres/oidc_settings.go): metadata remains in existing app settings, client secret in the existing stored-secret table with its registered OIDC name. Metadata/encrypted secret/value-free audit commit atomically. Single-statement reads bind metadata and secret to one database snapshot; unchanged secrets do not emit replacement secret audits. Migration [36](../../db/migrations/000036_authentication_settings_audit.up.sql) adds only value-free authentication-settings audit.
- Natural backend seams for ticket04: [service](../../internal/auth/admin_management.go), [API](../../internal/httpapi/local_management.go), [store](../../internal/store/postgres/local_management.go). Explicit one-account creation on upgrade, one-time generated credential response, forced change, disabled-state management, all-local-session revocation, atomic actor/target audit, and last-enabled-admin protection. Local role is locked to admin, and disabled built-ins do not satisfy last-admin counting. Migration [37](../../db/migrations/000037_local_admin_management_audit.up.sql) adds the value-free management audit. No ticket04/05 UI or deployment-ticket work.
- [OpenAPI](../../api/openapi.yaml) lands with all routes and schemas. [OIDC documentation](../../docs/OIDC_AUTH.md) and [D040 clarification](../../docs/DECISIONS.md) describe API semantics and operational limits without claiming a finished later-ticket UI. No new dependency or applied-migration edits.

## Red/green ledger

All paths below are ignored, private worker-local artifacts. Initial seam introduction reds include missing-public-API compilation failures; the later hardening/idempotency reds are behavioral regressions. No failed command is counted as a passing gate.

| Slice | Red (exit 1) | Green (exit 0) |
| --- | --- | --- |
| Pure unit precedence | `tmp/ticket03/state-red.log` | `tmp/ticket03/state-green.log` |
| Atomic discovered/durable save | `tmp/ticket03/service-red.log` | `tmp/ticket03/service-green.log` |
| Safe bounded discovery | `tmp/ticket03/discovery-red.log` | `tmp/ticket03/discovery-green.log` |
| PostgreSQL metadata/secret/audit transaction | `tmp/ticket03/persistence-red.log` | `tmp/ticket03/persistence-green.log` |
| Optional startup/app seam | `tmp/ticket03/startup-red.log` | `tmp/ticket03/startup-green.log` |
| In-flight original provider and issuer | `tmp/ticket03/snapshot-red.log` | `tmp/ticket03/snapshot-green.log` |
| Public minimal options/API seam | `tmp/ticket03/api-red.log` | `tmp/ticket03/api-green.log` |
| Generic-secret bypass and lost-key disable | `tmp/ticket03/hardening-red.log` | `tmp/ticket03/hardening-green.log` |
| Recovery management service/PostgreSQL | `tmp/ticket03/management-red.log` | `tmp/ticket03/management-green.log` |
| Recovery-management API/access | `tmp/ticket03/management-api-red.log` | `tmp/ticket03/management-api-green.log` |
| Idempotent repeated save | `tmp/ticket03/idempotency-red.log` | `tmp/ticket03/idempotency-green.log` |

An interim whole-unit run correctly found undocumented routes and registry-order assumptions in existing secret tests; routes were documented and registry order preserved before passing the contract and whole-suite gates. A shell command ending with a no-match failure grep returned exit 1 after the unit suite itself exited 0; that shell status was not treated as test evidence. Additional verification covers saved-only restart, diagnostic failure retention, disabling with a lost key, failed enable of an undiscovered disabled candidate, malformed/oversized request rejection, and a production no-OIDC server process with real PostgreSQL and successful forced-change local sign-in.

## Final executed validation

| Command | Result | Private artifact |
| --- | --- | --- |
| `make fmt` | Pass; tracked tree unchanged | `tmp/ticket03/final-fmt.log` |
| `make test` | Pass, also rerun by full check and pre-commit | `tmp/ticket03/final-test.log` |
| `make vet` | Pass, also rerun by full check and pre-commit | `tmp/ticket03/final-vet.log` |
| `make web-check` | Pass, including browser-test TypeScript | `tmp/ticket03/final-web-check.log` |
| `bash scripts/check.sh` | All Go, deployment, export, PostgreSQL, frontend gates pass; **248 passed (1.0m)**; no skipped gate | `tmp/ticket03/final-check.log` |
| `go test -race -count=1 -timeout=180s ./internal/auth/... ./internal/authsettings/... ./internal/app/... ./internal/httpapi/...` | Pass | `tmp/ticket03/final-race.log` |
| `go test -count=1 -timeout=180s -json ./...` | **542 top-level tests**, 1738 pass events including subtests; **0 failures, 0 skips** | `tmp/ticket03/final-unit-json.log` |
| `go test -race -tags integration -count=1 -timeout=240s -json ./internal/store/postgres/...` | **124 top-level tests**, 158 pass events including subtests; **0 failures, 0 skips** | `tmp/ticket03/final-integration-json.log` |
| `make build` | Production web assets and static binary build pass | `tmp/ticket03/final-build.log` |
| `git diff --check` | Pass | Rechecked before commits |
| Staged credential/build-artifact scan | Pass; generated database credential and private/build outputs absent | Before implementation and resolution commits |

The final full check and race/JSON runs were performed after committing the implementation and incorporating the latest integration revision. Ordinary no-test-file package entries are not skipped tests. PostgreSQL tests ran against real PostgreSQL, not mocks. Gate ledger: `tmp/ticket03/gates.tsv`; implementation changed-file manifest: `tmp/ticket03/changed-files.tsv`.

## Resources, limitations and separate-merger instructions

- Linux; Go 1.27.3, Node 26.13.0, npm 12.2.1, Docker 29.9.2, Compose 5.7.0. Node CI/Docker consistency check passes; no claim of executing under CI's Node 22.
- Owned PostgreSQL 17 container `requestarr-ticket03-20261105`, dynamically allocated loopback-only port, dedicated `requestarr_test` database. Only this owned database was recreated between suites. Fresh-store entrypoint tests create/drop their own databases there. Existing database services were untouched.
- Playwright used port 4299 and its own managed Vite process. Dependencies were installed inside this worker; no existing browser server was reused or stopped.
- Raw output redirected beneath mode-0700 `tmp/ticket03/`. Generated database credentials are only in ignored mode-0600 `postgres.env`/`validation.env`; do not publish raw logs or source private environment files under shell tracing. No generated credential was printed in assistant output or committed. Keep the worker/resources for merger verification; cleanup must target owned resources only.
- Browser tests are fixture-backed regression checks, not ticket04/05 UI acceptance. The signed OIDC fixture proves protocol behavior, not a live Keycloak/HTTPS/reverse-proxy/Unraid deployment. No such live acceptance claim is made.
- Discovery proves metadata, not client-secret validity; the first real sign-in can still fail for provider-side client/redirect policy. “Working” is active snapshot state, not continuous provider health. A lost encryption key requires restoration or re-entering the secret; disabling and discard still work. Restart invalidates pending in-memory OIDC snapshots.
- Backend handoff and merger instructions: [ticket03 handoff](ticket03-handoff.md). **Do not merge into integration as part of this worker task.** No PR was created and no root/unrelated-worker changes were made.
