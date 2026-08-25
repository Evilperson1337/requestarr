# Implementation Roadmap

This roadmap turns the current scaffold into the modular monolith described in [Architecture](ARCHITECTURE.md). It preserves the accepted decisions in the [decision log](DECISIONS.md), the product boundary in [Project Vision](PROJECT_VISION.md), and the required behavior in [V1 Scope](V1_SCOPE.md). Implement each phase as small, independently reviewable increments rather than as one feature branch.

## Non-negotiable boundaries

- Go 1.26 is the unified minimum and current toolchain for the module, development container, CI, production build, and releases. D013's earlier Go 1.23 baseline is superseded because reviewed critical vulnerabilities in the direct PostgreSQL driver materially invalidated it.
- Requestarr remains request, availability, and notification software. It does not acquire media or integrate with download clients, indexers, Usenet/torrent clients, or *arr applications.
- OIDC remains required for normal operation. The setup token is a temporary bootstrap mechanism, not local-password authentication.
- Configured libraries are authoritative for availability. Metadata providers identify catalog items but never establish availability.
- Routine UI rendering uses local PostgreSQL state. External provider, library, OIDC discovery, and Apprise failures must be bounded and must not block ordinary pages that can use known local state.
- A failed or incomplete library sync must retain last-known inventory and must never mass-mark previously available content missing.
- Applied migrations are immutable. Schema corrections and evolution use new, ordered migrations; rollback files do not justify editing a migration after it has merged.
- Domain policy remains independent of HTTP, PostgreSQL, and external-service clients. State changes are idempotent, auditable, and transactional where required.

## Current foundation assessment

The repository is a coherent, buildable scaffold, not yet a production service.

### Present

- A single Go entry point validates typed HTTP, logging, and required PostgreSQL URL settings without disclosing credentials, installs signal cancellation, and delegates process composition to the application package ([entry point](../cmd/requestarr/main.go), [runtime configuration](../internal/config/config.go), [application composition](../internal/app/app.go)).
- Startup constructs and pings a long-lived `pgx` pool, applies embedded migrations before listening, and closes database resources during shutdown ([PostgreSQL adapter](../internal/store/postgres/), [embedded migrations](../db/migrations/embed.go)).
- The HTTP server has bounded server timeouts and graceful shutdown. It exposes separate liveness and database-backed readiness probes, build information, the public persistent completion-only setup gate, the temporary setup-token-to-session exchange, and a temporary HTML root ([HTTP router](../internal/httpapi/router.go)).
- The HTTP transport now provides request correlation, stable JSON API errors for unknown routes and unsupported methods, secret-safe status-aware access logs, strict bounded JSON decoding, and a reusable no-store policy without introducing a router framework ([HTTP API package](../internal/httpapi/)).
- Domain types and narrow integration seams exist for requests, OIDC, metadata, inventory, reconciliation, notifications, jobs, setup, and persistence under [internal packages](../internal/).
- The initial PostgreSQL schema models users, canonical catalog items, one shared request per item, subscriptions, events, authoritative library sources, normalized inventory, notification deliveries, and resumable setup state ([initial migration](../db/migrations/000001_initial.up.sql)).
- Development definitions provide PostgreSQL 17, Keycloak, and Apprise API, including a PostgreSQL health check ([development Compose file](../compose.dev.yaml)).
- A React/TypeScript UI scaffold, OpenAPI seed, container/release files, unit tests, real PostgreSQL integration coverage with random-schema isolation, and required Make targets are present ([web application](../web/), [OpenAPI contract](../api/openapi.yaml), [testing guide](TESTING.md), [Makefile](../Makefile)).

### Missing or incomplete

- The public completion gate and temporary setup-session exchange are implemented. Richer session-authenticated setup reads and mutations, transactional configuration completion, OIDC, and the setup UI remain pending.
- The encryption-key lifecycle, AES-256-GCM envelope primitive, constrained encrypted-secret schema/repository, and fail-before-listen encrypted-row key preflight are implemented. Secret-writing setup/API flows and online key rotation remain pending.
- Provider, library, notification, and job packages are boundaries or placeholders rather than production adapters.
- The Go server does not serve the built React application. The root route still serves scaffold HTML.
- Production frontend artifact selection, backup/upgrade documentation, and production container validation remain outstanding.

## Decisions required before code

The Phase 1 gate is resolved by [D013 in the decision log](DECISIONS.md#d013--phase-1-production-persistence-contracts-2026-08-14). The contracts below are retained as roadmap context; implementation must follow D013 where it is more specific.

1. **PostgreSQL driver and pool.** Prefer `pgx/v5` with `pgxpool` for explicit PostgreSQL support, context-aware operations, pool statistics, and no ORM. Confirm the minimum supported PostgreSQL version; the current development baseline is PostgreSQL 17.
2. **Migration library.** Choose a library that can read an embedded filesystem, tracks ordered versions in PostgreSQL, fails on dirty/partial state, and supports testable up migrations. Prefer a small established runner over a custom migration engine.
3. **Migration ownership.** The application process should apply embedded up migrations before becoming ready. Do not require migration files beside the deployed binary. Define whether concurrent replicas rely on the migration library's PostgreSQL locking or whether deployment policy allows exactly one migrator.
4. **Startup failure policy.** Invalid database configuration, an unreachable database after a bounded startup attempt, migration failure, or incompatible/dirty schema should stop startup with a non-zero exit. Avoid infinite retry loops that hide deployment faults; orchestrators can restart the process.
5. **Probe contract.** Keep `/healthz` as process liveness with no external call. Add `/readyz` for readiness, returning success only after migrations complete and a bounded PostgreSQL ping succeeds. Decide a small probe timeout and a stable, non-secret JSON error shape.
6. **Configuration source and precedence.** The current runtime reads environment variables only while a YAML example also exists. Do not imply YAML loading until implemented. For the first increment, make `REQUESTARR_DATABASE_URL` required and define environment-only behavior; decide broader file/environment/flag precedence before expanding configuration.
7. **Migration rollback policy.** Retain down files for local/test convenience only if the selected runner uses them, but make production upgrades forward-only. Applied up files remain immutable.
8. **Frontend production artifact.** Before the production web-delivery phase, choose either compile-time embedding into the Go binary or a version-locked artifact copied into the production image. Prefer embedding when it preserves cross-platform single-binary releases without complicating local Vite development.

## Recommended package direction

Keep the composition root small and dependencies directed inward.

- [application package](../internal/app/) owns startup order, dependency lifetime, HTTP server construction, background-service start/stop, and graceful shutdown. It should compose concrete adapters but contain no request or availability policy.
- [configuration package](../internal/config/) parses and validates deployment inputs into typed settings. Secret-bearing values must never be rendered in validation errors or structured logs.
- Add a focused PostgreSQL adapter under `internal/store/postgres/`. It should own pool creation, ping behavior, transaction helpers, and concrete repositories as those repositories are introduced. Keep use-case-specific interfaces near the consuming domain/application service; do not expose a generic ORM or a database-shaped domain API.
- Add a small migration package, such as `internal/store/postgres/migrations/`, which embeds [migration assets](../db/migrations/) and exposes an application-level `Up` operation. The embedding declaration should live close enough to the assets for a valid relative pattern; if Go's embedding path rules require a package beneath [database assets](../db/), keep only asset exposure there and retain migration execution policy in the internal PostgreSQL adapter.
- [HTTP API package](../internal/httpapi/) remains transport-only. Construct probe handlers with an injected readiness checker rather than importing the PostgreSQL implementation directly.
- [jobs package](../internal/jobs/) should later coordinate persistent job claims and bounded workers; it must start only after database readiness and stop before the pool closes.
- Provider and library implementations remain adapters behind the existing interfaces. Their cached results and sync state belong in PostgreSQL, not in request-time global calls.
- [command entry point](../cmd/requestarr/main.go) remains limited to flags, configuration, logging, signal setup, and process exit behavior.

## Phased implementation

### Phase 1 — PostgreSQL startup, embedded migrations, and readiness

This is the single best next task. Every V1 workflow depends on durable local state, and implementing auth, setup, requests, jobs, or inventory first would either create temporary storage or force immediate rewrites. This increment also establishes the startup and test patterns later adapters will reuse.

**Scope**

- Add a typed, required database URL setting sourced from `REQUESTARR_DATABASE_URL`; validate presence without logging the URL.
- Add the selected PostgreSQL driver/pool and migration dependencies.
- Embed the existing migrations into the application artifact and apply pending up migrations during startup.
- Establish a bounded startup sequence: load/validate configuration, open pool, ping PostgreSQL, run migrations, build handlers, then listen.
- Add separate liveness and readiness probes. Liveness reports that the process can serve HTTP; readiness depends on completed startup and a short, context-bounded PostgreSQL check.
- Close the pool during orderly shutdown after HTTP and future workers stop.
- Return actionable but secret-free startup errors and non-zero process exit on database or migration failure.
- Add unit and PostgreSQL integration tests, including migration behavior.
- Update the API and operator documentation only where the implementation changes an existing contract.

**Explicit non-goals**

- No user, setup, request, inventory, job, or notification repositories.
- No OIDC flow, authorization middleware, setup-wizard behavior, provider client, library sync, reconciliation, or Apprise delivery.
- No schema redesign unless the existing migration cannot execute on the supported PostgreSQL baseline. If a merged migration is already considered applied, fix it with a new migration rather than editing history.
- No production frontend serving, UI changes, acquisition integration, or generalized framework introduction.
- No automatic database creation, privileged administrative connection, or destructive reset command.

**Likely affected packages and files**

- [Go module](../go.mod) and generated checksum file for selected dependencies.
- [runtime configuration](../internal/config/config.go) and [configuration tests](../internal/config/config_test.go).
- [application composition](../internal/app/app.go) and focused application startup tests.
- [HTTP router](../internal/httpapi/router.go) and probe tests.
- [store package](../internal/store/) plus a new PostgreSQL adapter and embedded migration package.
- [migration assets](../db/migrations/) only if a new append-only corrective migration is genuinely required; otherwise leave SQL unchanged.
- [OpenAPI contract](../api/openapi.yaml), [environment example](../.env.example), [README](../README.md), and [testing guide](TESTING.md) only as needed to describe the implemented startup/probe contract.
- [development Compose file](../compose.dev.yaml) or [CI workflow](../.github/workflows/ci.yml) only if required to run the new integration tests consistently.

**Acceptance criteria**

- With a valid empty PostgreSQL database, one Requestarr process starts, applies all embedded up migrations, and serves HTTP without reading migration files from disk.
- A second start against the migrated database is idempotent and does not alter or reapply completed versions.
- Missing or malformed database configuration fails validation before listening and does not disclose credentials.
- Unreachable PostgreSQL, a migration error, or dirty/incompatible migration state prevents readiness and causes bounded startup failure with a non-zero exit.
- `/healthz` remains responsive while the HTTP server is running and does not query external services.
- `/readyz` returns success only when startup migrations have completed and PostgreSQL responds within the configured internal timeout; loss of database connectivity makes readiness fail without crashing the process.
- Probe responses and logs reveal no database URL, password, tokens, cookies, or authorization headers.
- Shutdown stops accepting requests, waits within the existing bound, and closes database resources without a goroutine leak or panic.
- The binary behaves the same when launched outside the repository, demonstrating that migrations are embedded.

**Required tests**

- Configuration unit tests for default/required values, malformed URLs, sanitized errors, and accepted PostgreSQL URLs.
- Migration integration test against a real PostgreSQL instance: clean database to latest version, expected core tables exist, rerun is a no-op, and version state is clean.
- Startup integration tests for unavailable PostgreSQL and migration failure/dirty state, with bounded completion.
- Probe handler tests for liveness, ready, not-ready, checker timeout, and response content type/status.
- Application lifecycle test for cancellation and dependency closure where practical without binding fixed ports.
- Concurrency test or documented library guarantee for two processes attempting the same migration set.
- Run the race detector for the new lifecycle/readiness state even if it is not yet a mandatory repository-wide CI target.

### Phase 2 — Persistent setup state and OIDC bootstrap

The read-only persistent completion gate accepted in [D014](DECISIONS.md#d014--persistent-setup-status-read-contract-2026-08-14) is implemented: unauthenticated `GET`/`HEAD /api/v1/setup/status` exposes exactly the Boolean `complete` field, requires no setup token, returns a stable non-secret unavailable error instead of guessing on repository or persisted-state failure, and uses the existing immutable `000001` singleton without a migration.

The temporary bootstrap-session boundary governed by [D015](DECISIONS.md#d015--temporary-bootstrap-session-security-contract-2026-08-14) is implemented: durable migration/persistence followed by configuration, policy, and the HTTP exchange. Protected state, General and encrypted Authentication saves, and revision-bound hardened OIDC discovery validation are implemented. Test login, callback/token exchange, runtime sessions, transactional completion, later setup steps, and the remaining wizard UI continue as independently reviewable increments.

#### Phase 2B.1 — Setup session migration and persistence

**Status:** Implemented and retained as the persistence foundation for subsequent setup workflow increments.

**Implementation handoff**

- Add append-only `000002_setup_bootstrap_sessions.up.sql` and matching local/test-only down migration exactly as specified by D015; do not edit `000001`.
- Add focused PostgreSQL repositories for session digest creation/lookup/revocation, fixed-window rate counters, bounded cleanup, and setup security audit events. Keep transaction ownership behind setup-specific operations rather than exposing the pool or a generic repository.
- Use PostgreSQL transaction time and row locking. Provide one exchange transaction operation that locks the setup singleton, distinguishes incomplete/complete/invalid state, atomically commits invalid-attempt counters, and atomically inserts a successful session plus audit event. Provide a completion transaction seam that can later revoke all sessions and audit completion in the same unit as final setup writes.
- Do not add an HTTP route, cookie, token configuration, or runtime behavior in this persistence-only increment.

**Acceptance criteria and required tests**

- Clean PostgreSQL applies `000001` then `000002`; rerun is a no-op; expected tables, checks, foreign key, unique digest, primary key, and cleanup indexes exist; migration history is clean; down/up works only in the integration harness.
- Constraint tests reject non-32-byte digests, invalid scopes/counts/timestamps, duplicate session digests, and duplicate rate keys for one window while allowing the same key in a later window.
- Real-PostgreSQL concurrency tests prove atomic per-peer/global counter increments, exactly one rate-limit activation event per scope/key/window, completion locking versus exchange, and rollback without a session/event on injected failure.
- Repository tests prove expiry/revocation checks use database time; revocation is idempotent; bounded cleanup cannot remove active rows; persisted rows contain no raw token, session, IP, cookie, CSRF, or Authorization material; and cancellation/unavailability maps to secret-safe setup errors.

#### Phase 2B.2 — Token policy and session exchange endpoint

**Status:** Implemented and retained as the temporary setup capability boundary.

**Implementation handoff**

- Extend typed environment configuration with the unexported setup token and strict production/local-public-URL cookie policy from D015. Add standard-library helpers for canonical base64url validation, HMAC domain separation, constant-time verification, cryptographic session generation, origin normalization, and canonical direct-peer IP extraction.
- Add setup application policy around a narrow exchange transaction interface. Keep raw credentials and cookie mechanics out of PostgreSQL code, concrete SQL out of setup policy, and all method/body/header/cookie/error translation in the HTTP package.
- Implement only `POST /api/v1/setup/session`, `Allow: POST`, the exact D015 status/code/message precedence, durable limits, the 204 cookie response, no-store behavior, and existing correlation/security middleware. Preserve D014's public status route unchanged. Update OpenAPI and deployment examples in this runtime increment, not during Phase 2B.1.
- Do not add richer wizard reads/mutations, OIDC, reset/recovery, a logout/refresh route, proxy-header trust, CORS, new dependencies, or frontend behavior.

**Acceptance criteria and required tests**

- Configuration tests cover missing, malformed, padded, hex, UUID, whitespace, short, long, and non-canonical tokens; accepted 32-byte base64url; all redaction formatters/errors; HTTPS production; rejected production/non-loopback HTTP; and the exact loopback development exception.
- Unit tests use deterministic injected randomness/clock boundaries where appropriate and prove HMAC domain separation, constant-time verifier path for accepted-format candidates, opaque 32-byte session generation, keyed session/rate digests, no raw-secret persistence parameters, origin normalization/default ports, ignored proxy headers, and direct-peer IPv4/IPv6 handling.
- Handler tests cover exact success cookie attributes in production and local HTTP development; no body on 204; no cookie on every failure; malformed/body/content-type/method/origin/locked/completed/invalid/rate-limited/unavailable outcomes; `Retry-After`; request IDs/security headers/no-store; unchanged public status; and rejection of tokens in headers/query/cookies.
- Real-PostgreSQL endpoint tests cover restart persistence, two application instances sharing sessions and limits, concurrent valid exchanges yielding independently valid sessions, exchange/completion ordering, token rotation invalidating old sessions, expiry/revocation/completion lockout, atomic rollback, cleanup bounds, and audit allowlists.
- Secret-capture tests seed distinctive token/session/IP/header/body/database values and prove they are absent from logs, API bodies, audit JSON, database columns other than keyed digests, and formatted configuration/errors. Run `go test -race` over setup/config/HTTP/application/store packages plus the repository-required checks.

**Scope**

- The PostgreSQL-backed completion-only public read defined by D014 and the high-entropy setup-token exchange for short-lived server-side sessions defined by D015 are implemented.
- Next, add richer session-authenticated setup reads and mutations plus transactional configuration completion and bootstrap-session revocation; do not treat the persistence completion seam alone as a complete configuration workflow.
- Then implement OIDC discovery, authorization code flow with PKCE/state/nonce validation, secure sessions, Keycloak compatibility, role mapping, and server-side authorization.
- Persist users by issuer/subject-compatible identity and expose only the minimum setup/auth API required by the still-pending UI.

**Explicit non-goals**

- No permanent local accounts or password login.
- No metadata resolution, request workflow, library sync, or notification delivery.
- No unauthenticated network-accessible setup wizard.

**Likely areas**: [setup package](../internal/setup/), [OIDC package](../internal/auth/oidc/), [HTTP API](../internal/httpapi/), PostgreSQL repositories, [OpenAPI contract](../api/openapi.yaml), and setup/auth UI routes.

### Phase 3 — Metadata resolution and canonical catalog

**Scope**

- Implement strict link recognition and canonical resolution for TMDB, MusicBrainz, IGDB, and Audible/Audnexus behind narrow interfaces.
- Cache canonical records locally, validate untrusted payloads, enforce bounded HTTP behavior, and preserve media-specific artwork proportions.
- Add explicit connection tests and stale cached reads where appropriate.

**Explicit non-goals**

- No native search, discovery, trending, acquisition, or library-derived availability.
- No provider call on ordinary shared-request page rendering.

**Likely areas**: [metadata providers](../internal/providers/metadata/), a catalog domain/application package, PostgreSQL repositories, sanitized fixtures, API contract, and request-modal UI.

### Phase 4 — Shared requests, subscriptions, and audit history

**Scope**

- Implement one request per canonical item, creator auto-subscription, Notify Me/unsubscribe, role-aware transitions, and audit events.
- Keep request status and availability distinct internally even where UI badges simplify them.
- Implement shared, bookmarkable Requests filters, including My Requests as a filter only.

**Explicit non-goals**

- No duplicate per-user request rows, votes, private queues, acquisition action, or library inference.
- No automatic availability transition until authoritative inventory exists.

**Likely areas**: [request domain](../internal/domain/request.go), application services, PostgreSQL repositories, [HTTP API](../internal/httpapi/), [OpenAPI contract](../api/openapi.yaml), and shared Requests UI.

### Phase 5 — Persistent jobs and authoritative library inventory

**Scope**

- Add durable, idempotent job runs and bounded background scheduling.
- Implement paginated/delta-capable Plex, Jellyfin, Navidrome/OpenSubsonic, RomM, and Audiobookshelf adapters behind inventory interfaces.
- Commit a source snapshot/removal decision only after a successful authoritative cycle; preserve last-known inventory on timeout, parse error, cancellation, or partial enumeration.
- Emit normalized change events and expose operational sync status from local state.

**Explicit non-goals**

- No metadata provider as an availability source.
- No download client, indexer, *arr, file import, rename, or game-install integration.
- No full-library/full-request N×M scan on each change.

**Likely areas**: [jobs package](../internal/jobs/), [inventory package](../internal/inventory/), source adapters, PostgreSQL schema/repositories, fixtures, and settings/status UI.

### Phase 6 — Targeted reconciliation and notifications

**Scope**

- Reconcile changed inventory against candidate active requests using stable external IDs first and reviewable fallback confidence only when necessary.
- Implement IGDB platform `Any` or explicit multi-select OR fulfillment semantics.
- Implement audiobook reconciliation using exact ASIN/region and stable external IDs first, with reviewable narrator-aware fallback matching only when necessary.
- Emit idempotent availability events and create subscriber-specific in-app/Apprise deliveries with bounded retries and deduplication.

**Explicit non-goals**

- No availability changes after failed source syncs.
- No global rescan for every item, notification fan-out to non-subscribers, or destination-specific notification adapters beyond Apprise API.

**Likely areas**: [reconciliation package](../internal/reconcile/), [notification package](../internal/notifications/), [Apprise adapter](../internal/notifications/apprise/), jobs, repositories, fixtures, and notification UI.

### Phase 7 — Complete setup/settings and production web delivery

**Scope**

- Finish the complete resumable setup sequence and settings/status screens after their server-side contracts exist.
- Pin frontend dependencies with a committed lockfile and make frontend checks reproducible.
- Build the React application as a version-locked production artifact, serve static assets with correct content types/cache policy, and provide safe SPA fallback without shadowing `/api/`, `/healthz`, or `/readyz`.
- Preserve independent Vite development and make release/container builds fail when the expected frontend artifact is absent or stale.

**Explicit non-goals**

- No server-rendered duplicate UI, runtime npm installation, CDN dependency, or silent fallback to scaffold HTML in production.
- No pixel-perfect override of accessibility, responsiveness, or native artwork proportions.

**Likely areas**: [web application](../web/), [HTTP API](../internal/httpapi/), a focused web-asset package, [Dockerfile](../Dockerfile), [Makefile](../Makefile), [GoReleaser configuration](../.goreleaser.yaml), and CI/release workflows.

### Phase 8 — Hardening and release

**Scope**

- Complete threat modeling, secret-at-rest strategy, CSRF/rate limits, SSRF controls, audit review, query/index analysis, race/load tests, backup/restore and forward-upgrade documentation, container hardening, SBOM/provenance, and release validation.
- Validate end-to-end source-outage behavior and document blocking versus follow-up release issues.

**Explicit non-goals**

- No V1 scope expansion while hardening.
- No stability claim until migration/backup recovery and production artifact delivery are exercised.

**Likely areas**: [security baseline](SECURITY.md), [release guide](RELEASES.md), [Dockerfile](../Dockerfile), CI/release workflows, deployment documentation, and focused implementation packages identified by review findings.

## Migration strategy

1. Treat [initial migration](../db/migrations/000001_initial.up.sql) as immutable once merged/applied in a shared environment.
2. Embed migration content into the release artifact and record/check checksums if supported by the selected runner.
3. Run forward migrations before readiness and before starting jobs. HTTP must not advertise readiness against an older schema.
4. Serialize concurrent migration attempts with a PostgreSQL advisory lock or the selected library's equivalent. Set a bounded lock/startup timeout and emit a sanitized diagnostic on failure.
5. Execute each migration transactionally when PostgreSQL and the statement set permit it. For future non-transactional operations, isolate them in a deliberately reviewed migration and document recovery.
6. Never infer success from partial schema inspection. Use the migration version table and reject dirty state.
7. Add a new sequential migration for every schema correction. Include migration tests and update repository/query tests in the same increment.
8. Favor expand-and-contract changes for production compatibility: add nullable/defaulted structures, deploy compatible code, backfill with bounded jobs if needed, then constrain/remove only in a later release.
9. Treat down migrations as developer/test recovery aids, not the normal production rollback mechanism. Production rollback should restore a tested database backup with a compatible application version.
10. Before the first stable release, document backup, restore, upgrade ordering, supported version jumps, and operator recovery from failed migration startup.

## Production web-delivery sequencing

Do not make frontend embedding the first infrastructure task. Use this order:

1. Establish PostgreSQL-backed startup/readiness and deterministic server lifecycle.
2. Stabilize setup/OIDC and the API route namespaces so SPA fallback cannot interfere with security-sensitive or operational endpoints.
3. Implement feature APIs and keep Vite as the development server while the UI evolves.
4. Pin frontend dependencies and commit the lockfile; make typecheck/test/build reproducible in CI without installing dependencies opportunistically at runtime.
5. Select and implement one version-locked artifact path. Prefer compile-time embedding for a single-binary release, or deliberately copy the exact built artifact into the production image if release constraints favor image-only delivery.
6. Add static-asset cache headers using content-hashed assets, no-cache handling for the HTML shell, correct MIME types, and SPA fallback limited to non-API GET/HEAD routes.
7. Build the web artifact before the Go release build, verify that missing assets fail the build, and exercise the resulting binary/image without the source tree present.
8. Only then replace the scaffold root, update container/release packaging, and add production smoke tests for deep links, API 404s, probes, and immutable assets.

## Specialist routing

| Increment | Primary specialist | Required review/support |
| --- | --- | --- |
| Phase 1 startup/migrations/readiness | Go Data Persistence Engineer | Go Architect for dependency/startup contract; Go Test Engineer for PostgreSQL integration tests; Go Terminal Engineer for validation |
| Phase 2 setup/OIDC | Go API Engineer | Go Security Reviewer and Go Test Engineer; Go Web UI Engineer for setup flow |
| Phase 3 metadata/catalog | Go API Engineer | Go Security Reviewer for outbound HTTP/SSRF and Go Test Engineer for fixtures/failures |
| Phase 4 requests/subscriptions | Go Implementer | Go API Engineer, Go Data Persistence Engineer, and Go Test Engineer |
| Phase 5 jobs/libraries | Go Data Persistence Engineer | Go Performance Engineer, Go API Engineer, and Go Test Engineer |
| Phase 6 reconciliation/notifications | Go Implementer | Go Performance Engineer, Go API Engineer, and Go Test Engineer |
| Phase 7 production web delivery | Go Web UI Engineer | Go Release Engineer, Go Security Reviewer, and Go Terminal Engineer |
| Phase 8 hardening/release | Go Release Engineer | Go Security Reviewer, Go Performance Engineer, Go Code Reviewer, and Go Documentation Writer |

Route every schema increment through persistence and test review. Route every authentication or externally reachable integration increment through security review. Use deterministic fakes and fixtures rather than making live external services part of ordinary CI.

## Validation expectations

Every implementation increment must run the repository-required checks:

```bash
make fmt
make test
make vet
make web-check
```

Run `make web-check` once frontend dependencies are already installed; do not perform an unrelated dependency installation merely to satisfy a backend-only increment. Also run focused checks appropriate to the change. For Phase 1, require at minimum:

```bash
go test -race ./internal/app/... ./internal/config/... ./internal/httpapi/... ./internal/store/...
docker compose -f compose.dev.yaml up -d postgres
go test -tags=integration ./internal/store/...
docker compose -f compose.dev.yaml down
git diff --check
```

The final integration-test command and build tag may be adjusted to match the implemented test harness, but PostgreSQL migration tests must run against real PostgreSQL rather than an in-memory substitute. CI should own repeatable service startup; tests should use isolated databases or schemas and must not destroy a developer's configured database.

## Principal risks and mitigations

- **Migration race or partial state:** use a proven runner, database locking, bounded startup, transactional migrations, dirty-state rejection, and real concurrent-start tests.
- **Secret leakage through errors:** parse configuration without echoing DSNs; sanitize driver errors and structured fields; test logs and responses with credential-bearing input.
- **Probe semantics causing restart loops:** keep liveness dependency-free, put PostgreSQL checks only in readiness, bound checks tightly, and document status behavior.
- **External-service coupling:** keep provider/library calls out of routine reads; use cached PostgreSQL state, background jobs, timeouts, and stale-but-known results.
- **False inventory removals:** distinguish a completed authoritative snapshot from timeout/cancellation/partial enumeration and apply removals only after success.
- **Domain/database coupling:** define interfaces around use cases, use explicit SQL in adapters, and keep transaction orchestration at application-service boundaries.
- **Initial schema drift before implementation:** do not casually rewrite the initial migration. Determine whether it has been applied in shared environments; prefer an append-only correction when uncertain.
- **Frontend artifact drift:** pin dependencies, produce assets before Go packaging, fail on missing artifacts, and test the release artifact outside the source tree.
- **Scope creep toward acquisition:** reject dependencies, UI actions, or events that enqueue downloads or manage *arr/indexer clients; availability remains observational and library-authoritative.
- **OIDC shortcuts during setup:** keep bootstrap capability temporary and narrow; test unauthorized, viewer, requester, and admin paths before exposing mutating APIs.

## First increment issue and pull request

**Suggested issue title**

`foundation: add PostgreSQL startup, embedded migrations, and readiness`

**Suggested issue scope**

- Select and document the pool/migration libraries and probe contract.
- Add required database configuration with secret-safe validation.
- Open/ping PostgreSQL, apply embedded migrations, then start HTTP.
- Add dependency-aware `/readyz` while preserving dependency-free `/healthz`.
- Add lifecycle, probe, failure-path, and real PostgreSQL migration tests.
- Update only directly affected API/operator documentation.

**Suggested pull request title**

`feat(foundation): add PostgreSQL startup and readiness`

**Suggested branch name**

`feat/postgres-startup-readiness`

**Suggested Conventional Commit**

`feat(foundation): add PostgreSQL startup and readiness`

Keep the first pull request limited to the Phase 1 acceptance criteria. Repository implementations, setup persistence, OIDC, and UI delivery should follow in separate reviewable increments.

## Audiobook V1 extension checkpoint

Treat audiobook support as an extension of the existing metadata → catalog → request → inventory → reconcile pipeline, not a parallel subsystem. Add the media type and schema constraint changes through a new migration, then implement Audible/Audnexus resolution, Audiobookshelf source configuration/sync, exact-ID reconciliation, setup/settings UI, and end-to-end request/Notify Me/Available notification coverage. Do not start ebook adapters in this checkpoint.

## Post-V1 discovery checkpoint

After link-first V1 is stable, study Seerr/Libreseerr presentation and implement provider-concurrent universal search/discovery behind a normalized presentation model. External discovery latency may progressively enrich results, but request/availability/subscription state must be joined from local PostgreSQL and the existing universal item/request modal must remain the action surface.

## Chaptarr-derived audiobook reconciliation refinement (2026-08-19)

Before completing audiobook V1 reconciliation, implement the cross-media match-evidence contract from D024 and migration `000008_reconciliation_match_evidence`. Persist match method/confidence separately from request state. Automatic request -> Available transitions require `exact` or `high` confidence; `possible` candidates remain unresolved for admin review. Audiobook fallback matching must account for narrator and materially different productions. Do not introduce Chaptarr runtime integration or ebook acquisition/domain scope as part of this work.
