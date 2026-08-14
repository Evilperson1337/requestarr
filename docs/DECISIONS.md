# Architecture / Product Decision Log

These are accepted decisions unless explicitly superseded by a dated entry.

## D001 — No acquisition layer
Requestarr only manages requests, availability, and notifications. No *arr/downloader integration in V1.

## D002 — Link-first V1
Users paste metadata provider links instead of searching an internal/global catalog. Native discovery can be added later without changing the request modal/domain path.

## D003 — Shared single request
One canonical request exists per item. The requester's identity is attribution, not privacy/ownership. Requests are visible to every authenticated user.

## D004 — Notify Me subscriptions
The requester is subscribed automatically. Other users may subscribe to request state changes without creating a duplicate request or vote.

## D005 — OIDC required
OIDC is mandatory for normal V1 operation. Keycloak is the primary tested provider. First-run bootstrap uses a temporary setup mechanism only.

## D006 — Apprise integration
In-app notifications are native; external notifications use Apprise API instead of implementing many destination-specific adapters.

## D007 — Library authority
Plex/Jellyfin determine movie/TV availability, Plex/Jellyfin/Navidrome determine music availability, and RomM determines game availability. A source is authoritative only for configured media types.

## D008 — Local state + background sync
UI state is primarily served from local PostgreSQL. External slowness/outages should not block normal pages. Source failures retain last-known inventory.

## D009 — Game platforms are dynamic OR criteria
IGDB defines which platforms to display. UI shows Any first and then provider platforms as multi-select checkboxes. Any selected platform may satisfy the request.

## D010 — Native artwork proportions
Poster media remains ~2:3; album/song artwork remains square. UI normalizes layout containers, not the art itself.

## D011 — Shared Requests navigation
Primary navigation says `Requests`, not `My Requests`. My Requests is a filter/button on the shared page.

## D012 — Setup wizard is V1
First run guides General -> Authentication -> Metadata Providers -> Libraries -> Notifications -> Request Policy -> Review -> Finish, with external connection validation and resumability.

## D013 — Phase 1 production persistence contracts (2026-08-14)

**Status:** Accepted for the Phase 1 PostgreSQL startup, embedded migrations, and readiness increment.

**Scope:** This decision resolves the implementation gate only. It does not change any accepted product decision, schema, runtime behavior, or V1 boundary.

### Supported platform and module identity

- **Go baseline amendment (2026-08-14):** This paragraph supersedes D013's earlier Go 1.23 baseline. Security review verified three critical advisories affecting the directly used PostgreSQL driver version. The fixed `github.com/jackc/pgx/v5` v5.9.x line requires a newer Go baseline, so retaining Go 1.23 would preserve a material security blocker. The user selected Go 1.26 as Requestarr's unified minimum and current toolchain. The module declaration, development container, CI, production container build, and release workflow must therefore use stable Go 1.26 forms. Compatibility with Go versions earlier than 1.26 is no longer claimed.
- The production database baseline is PostgreSQL 17. The verified development service is PostgreSQL 17.11 from `postgres:17-alpine`. Compatibility with older PostgreSQL releases is not claimed until CI exercises them and a later decision broadens the support matrix.
- `github.com/example/requestarr` is a scaffold placeholder, not the canonical module identity. Before adding Phase 1 imports, the implementation increment must atomically change the module path and all repository-local imports to `git.example.com/evilperson1337/Requestarr`, derived from the configured Git origin. This is a source-identity correction, not a package redesign. It belongs in the same focused implementation change because leaving the placeholder would multiply invalid imports. If that repository URL is not intended to be the durable import path, repository ownership must choose the durable path before the persistence engineer adds code.

### PostgreSQL access

- Use `github.com/jackc/pgx/v5` and `github.com/jackc/pgx/v5/pgxpool` for runtime PostgreSQL access. The direct dependency is pinned to v5.9.2, the user-approved minimal-drift fixed release that clears the three reviewed critical driver advisories. Use explicit SQL and no ORM.
- The application owns one long-lived `pgxpool.Pool`. Pool creation, pinging, readiness checks, and eventual repositories belong under `internal/store/postgres`; domain packages and HTTP handlers must not import the concrete pool.
- Use the pgx standard-library adapter only for the migration runner's short-lived `database/sql` connection. It is not a second application query layer or long-lived pool, and it must be closed immediately after migration startup completes.

### Migration runner and embedded asset ownership

- Use `github.com/golang-migrate/migrate/v4`. It is established, supports the repository's existing paired `.up.sql`/`.down.sql` naming, supports an embedded `io/fs` source, records PostgreSQL version and dirty state, and has PostgreSQL advisory locking. This compatibility avoids rewriting the existing initial migration merely to satisfy a runner-specific file format.
- The Go package colocated with `db/migrations` owns the `embed.FS`, because Go embed patterns cannot traverse upward with `..`. That package exposes only the embedded migration filesystem. Migration execution policy, connection handling, timeout configuration, and error translation remain under `internal/store/postgres/migrations`.
- The release binary applies embedded **up** migrations before handlers are built and before the listener starts. It must not read migration files from the working directory. A no-change result is successful and repeated startup is idempotent.
- Concurrent application instances rely on the migration runner's PostgreSQL advisory lock; deployment does not require a singleton migrator. Migration lock acquisition is bounded to 15 seconds. Failure to acquire the lock within that bound fails startup.

### Migration history, dirty state, and checksums

- The runner's version table in the configured target schema is the authoritative migration history. A dirty version or a database version newer than the binary's embedded migration set is incompatible and fails startup with a sanitized error. Phase 1 must test clean first-run, no-op rerun, dirty state, and concurrent attempts.
- `golang-migrate` does not persist content checksums. Phase 1 will not add a custom checksum engine or a second history table. Applied migration files are immutable by repository policy; corrections use a new sequential migration. CI migration tests must apply the embedded set from a clean schema. A checksum manifest or runner change may be decided before the first stable release if release operations require runtime drift detection.
- Existing down files remain available for explicit local/test recovery, but the production application never invokes down migrations. Production schema evolution is forward-only. Application rollback requires a schema-compatible binary or restoration of a tested backup; it must not automatically migrate down.
- Each migration should be transactional when its PostgreSQL operations permit it. The existing explicit transaction remains unchanged in Phase 1. A future migration requiring non-transactional PostgreSQL operations must be isolated, reviewed, and accompanied by a documented recovery path.

### Bounded startup and failure behavior

- Startup order is: load and validate configuration; construct the pool; ping PostgreSQL; acquire the migration lock and apply embedded up migrations; close the migration connection; construct handlers; then listen. Future background workers start only after this sequence succeeds.
- Pool construction and initial ping share a 10-second context deadline. Migration lock wait is at most 15 seconds. Migration execution uses a 60-second PostgreSQL statement timeout per migration operation for Phase 1. These are internal constants, not new operator settings in this increment. Cancellation must be honored between phases; there is no infinite retry loop.
- Missing or invalid database configuration, inability to connect or ping within the deadline, lock timeout, migration failure, dirty history, or a database version ahead of the binary prevents the listener from starting and returns a non-zero process exit. Errors and logs identify the failed phase and stable category without including the database URL, credentials, SQL text, or raw secret-bearing configuration.
- The application does not create databases, grant privileges, reset schemas, or repair/force dirty migration versions automatically.

### Liveness and readiness

- `GET /healthz` is process liveness. It performs no database or external-service call and returns HTTP 200 with `{"status":"ok"}` while the HTTP server can respond.
- `GET /readyz` is readiness. It uses an injected checker with a fresh 2-second context deadline and returns HTTP 200 with `{"status":"ready"}` only after startup migrations completed and PostgreSQL responds to a ping. A timeout, cancellation, closed pool, or ping failure returns HTTP 503 with `{"status":"not_ready","reason":"database_unavailable"}`.
- Probe responses use `application/json`, expose no raw dependency error, and use only stable non-secret reason codes. A readiness failure after startup does not terminate the process and must not make liveness fail.
- Because Phase 1 completes required database initialization before opening the listener, no normal startup interval serves a transient not-ready HTTP response. The readiness state still explicitly records successful migration completion so tests and future lifecycle changes cannot accidentally advertise readiness early.

### Database configuration and redaction

- Phase 1 reads the database connection only from required `REQUESTARR_DATABASE_URL`. The YAML example is documentation only and gains no implicit precedence. Broader file/flag precedence remains undecided and out of scope.
- Validation accepts only absolute `postgres://` or `postgresql://` URLs that pgx can parse and that select a non-empty database name. Unsupported schemes, malformed URLs, and missing values fail before any network call. Driver-supported URL query parameters remain available; Phase 1 does not invent duplicate pool environment variables.
- The typed configuration must not implement secret-revealing string formatting. Validation errors name `REQUESTARR_DATABASE_URL` and the violated rule but never echo its value. Logs, probe bodies, wrapped startup errors, and test failure messages must not contain the URL, password, or userinfo. Any diagnostic representation must redact at least password and userinfo and should prefer phase/category fields over a reconstructed DSN.

### Integration-test isolation and safety

- PostgreSQL integration tests are opt-in through the `integration` build tag and require `REQUESTARR_TEST_DATABASE_URL`; they must never fall back to `REQUESTARR_DATABASE_URL` or production-like defaults.
- Each test run creates a cryptographically random schema named with the `requestarr_test_` prefix inside the explicitly supplied test database, configures all migration connections to use only that schema, and may run tests in parallel with independent schemas. Migration history is schema-local.
- Cleanup may `DROP SCHEMA ... CASCADE` only for the exact generated, quoted schema created by that test run. Tests must never drop a database, the `public` schema, a pre-existing schema, or objects outside their generated schema. If schema creation or target verification fails, tests fail closed without destructive cleanup.
- The test database role needs connect, create-schema, and ordinary object privileges only; superuser and database-owner assumptions are forbidden. CI and the verified devcontainer PostgreSQL 17 service may supply the explicit test URL. Concurrency coverage must exercise two migrators against the same generated schema, while isolation coverage uses different generated schemas.

### Consequences and implementation handoff

- Phase 1 adds only the canonical module-path correction, selected dependencies, typed database configuration, PostgreSQL pool/migration adapters, startup wiring, probes, and focused tests/documentation described in the roadmap. It adds no repositories or product workflow.
- The persistence engineer must preserve `internal/app` as the composition root, keep probe transport in `internal/httpapi`, keep concrete PostgreSQL details under `internal/store/postgres`, and expose narrow consumer-owned interfaces rather than a generic store abstraction.
- No blocker remains if `git.example.com/evilperson1337/Requestarr` is the intended durable module path. A different intended import path is the only decision that must be answered before implementation begins.
