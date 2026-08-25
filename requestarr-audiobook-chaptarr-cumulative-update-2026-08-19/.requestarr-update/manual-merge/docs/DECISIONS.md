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

## D014 — Persistent setup-status read contract (2026-08-14)

**Status:** Accepted for the next narrow setup increment after the Phase 1 foundation.

**Scope:** This decision resolves only the persistent, read-only setup-completion gate used by a client to choose setup versus normal login. It does not implement setup bootstrap sessions, setup mutation, OIDC, or the complete wizard.

### Public disclosure and HTTP contract

- `GET /api/v1/setup/status` remains callable without OIDC authentication or a setup bootstrap session because a client must choose whether to route to setup or login before either flow is available.
- A successful response is exactly HTTP 200, `Content-Type: application/json`, and `{"complete":false}` or `{"complete":true}`. `complete` is the only response field. Current step, resumable draft, validation results, configured values, and secret-bearing state are never disclosed by this public endpoint.
- `HEAD /api/v1/setup/status` has the same status and headers as `GET` and no response body. Other methods retain the existing HTTP 405 API error behavior and `Allow: GET, HEAD`. Successful and failed responses retain the existing request-correlation and no-store/security-header behavior.
- This read-only increment neither consumes nor requires `REQUESTARR_SETUP_TOKEN`. Token validation, short-lived bootstrap-session issuance, authorization of setup reads beyond completion, locked mutation after completion, token invalidation, and reset/recovery behavior belong to the next bootstrap-session increment. This exception for completion disclosure does not make the setup wizard or any setup mutation anonymously accessible.

### Persistence and invalid-state behavior

- Read `complete` from the one row identified by `setup_state.singleton = TRUE`; do not derive completion from current step, draft content, configuration tables, OIDC availability, or external-service validation.
- Exactly one singleton row with a valid non-null Boolean completion value is required. A missing row, duplicate/impossible singleton result, scan/type failure, or otherwise invalid persisted setup state fails explicitly and safely; it must not be interpreted as either complete or incomplete and must not route callers into an unprotected setup flow.
- A repository/database failure and invalid persisted state both return HTTP 503 through the standard JSON API error envelope with stable code `setup_status_unavailable` and non-secret message `Setup status is temporarily unavailable.` The response must not expose SQL, driver text, schema details, current step, draft, configuration, credentials, or tokens. Internal diagnostics may distinguish failure categories but must remain secret-safe.
- Do not cache or substitute a default when the authoritative read fails. The client must show an unavailable/retry state rather than guessing setup or login.

### Schema and implementation boundary

- The existing `setup_state` singleton in migration `000001` already supports this read. Migration `000001` remains immutable and this increment adds no migration.
- Keep the setup-state read behind a narrow consumer-owned interface. The PostgreSQL implementation belongs under `internal/store/postgres`; setup semantics remain under `internal/setup`; `internal/app` wires the concrete dependency; and `internal/httpapi` owns only HTTP method, response, and error translation.
- The implementation increment must replace the scaffold response and remove the public `scaffold` field, update the OpenAPI contract in the same change, and add focused repository, handler, application-wiring, and real-PostgreSQL tests. It must not add setup writes, token/session behavior, schema changes, or dependencies.

### Consequences and implementation handoff

- The browser receives only the minimum pre-authentication routing signal. Resumability remains a requirement, but resuming at a particular step requires a future authorized bootstrap-session API.
- Required failure-path coverage includes incomplete and complete singleton reads; unauthenticated access; exact response shape with no extra fields; `HEAD`; unsupported methods; missing/invalid persisted state; unavailable/cancelled database operations; secret-safe errors; and proof that no setup token is read or required.
- The next bootstrap-session increment must preserve this public completion-only read while requiring the temporary setup capability for every richer setup read, validation action, and mutation, and while rejecting mutations after completion except through an explicitly designed recovery path.

## D015 — Temporary bootstrap-session security contract (2026-08-14)

**Status:** Accepted for the setup bootstrap-session increment that follows D014.

**Scope:** This decision defines the complete temporary capability boundary used before OIDC is available. It does not implement the wizard's configuration payloads, OIDC, an administrative reset, or any runtime/schema change by itself. D014's public completion-only `GET`/`HEAD /api/v1/setup/status` remains unchanged.

### Deployment token and locked startup state

- `REQUESTARR_SETUP_TOKEN` accepts exactly one format: 32 random bytes encoded as unpadded base64url (RFC 4648 URL-safe alphabet), exactly 43 ASCII characters matching `^[A-Za-z0-9_-]{43}$`. Decoding must produce exactly 32 bytes, including rejection of non-canonical encodings. Operators should generate it from a cryptographically secure random source; passphrases, UUIDs, hexadecimal strings, padded base64, surrounding whitespace, and multiple/legacy formats are rejected. This provides 256 bits of generated entropy.
- The token is read only from the environment. It is never generated, persisted, printed, accepted from YAML/flags, or returned by Requestarr. Configuration holds it in an unexported secret-bearing field and its `String`/`GoString`, validation errors, and startup logs redact it.
- A missing or malformed token does **not** prevent the public status route, liveness, readiness, or a completed instance from starting. It puts an incomplete instance in a fail-closed `setup_locked` state: session exchange and every richer setup read, validation, and mutation are unavailable. There is no default token and no network bootstrap fallback.
- On a completed instance, the token is ignored for authorization and exchange always returns `setup_completed`. Restarting with a different token does not reset completion or revive sessions.

### Token transport and verification

- The sole token exchange is `POST /api/v1/setup/session` with exactly one `Content-Type: application/json` header and a body no larger than 128 bytes. Its only accepted document is `{"token":"<43-character token>"}`. The raw token is never accepted in `Authorization`, cookies, query parameters, path segments, form data, or any other header. This deliberate JSON-body choice keeps the bootstrap secret out of commonly instrumented authorization/header and URL surfaces while retaining the existing strict bounded decoder.
- At configuration load, decode the configured token and derive `setup_key = HMAC-SHA-256(key=configured_token_bytes, data="requestarr/setup/v1/key")`. Derive the configured verifier as `HMAC-SHA-256(key=setup_key, data="requestarr/setup/v1/bootstrap-token\x00" || configured_token_bytes)`. For an accepted-format candidate, derive the candidate verifier the same way and compare the 32-byte values with `crypto/subtle.ConstantTimeCompare`; do not compare strings. Format/length rejection may occur before the constant-time comparison. No raw token, reversible token value, or unsalted plain hash is stored in PostgreSQL.
- The same `setup_key` is the application pepper for session and rate-limit key digests. It remains process configuration, not database data. Rotation while setup is incomplete invalidates all existing setup sessions by making their digests unverifiable; this is acceptable emergency revocation, not a supported reset workflow.

### Opaque server-side setup session

- A successful exchange generates 32 bytes with `crypto/rand`, encodes them as an unpadded 43-character base64url value, and sends that value only as the cookie value. PostgreSQL stores only `HMAC-SHA-256(key=setup_key, data="requestarr/setup/v1/session\x00" || raw_session_bytes)`. Lookup recomputes this digest and uses the unique digest index; raw session material is never stored.
- A setup session has an absolute 15-minute lifetime from the PostgreSQL transaction timestamp. It has no idle extension or refresh endpoint. Every authorized operation checks that the persisted row exists, `revoked_at IS NULL`, `expires_at > transaction_timestamp()`, and authoritative `setup_state.complete = FALSE`.
- Expiry is logical and immediate even before physical cleanup. Setup completion revokes every unrevoked setup session in the same completion transaction. Explicit session logout and browser-driven reset are non-goals for this increment; discarding the cookie ends client use, and expiry bounds residual validity.
- Each successful exchange creates a new independent session. Exchange is intentionally non-idempotent: retries and concurrent valid exchanges each return a distinct cookie and all resulting sessions remain valid until expiry, revocation, or completion. The `setup_state` row lock makes exchange versus completion deterministic: completion holding or winning the lock prevents session creation; an exchange that commits first may create a session which the immediately following completion revokes.

### Cookie and transport contract

- The cookie name is `__Host-requestarr_setup`; `Path=/`; no `Domain`; `HttpOnly`; `SameSite=Strict`; `Max-Age=900`; and an `Expires` value equal to the server-side expiry. Responses carrying it use `Cache-Control: no-store`.
- Production and every non-loopback deployment require an HTTPS `REQUESTARR_PUBLIC_URL`; the setup cookie has `Secure=true`. The application does not infer external HTTPS from `Forwarded` or `X-Forwarded-*` headers.
- The only local HTTP exception is explicit development mode with an `http` public URL whose host is `localhost`, `127.0.0.0/8`, or `::1`. Because browsers reject a `__Host-` cookie without `Secure`, that exact mode uses the distinct cookie name `requestarr_setup_dev`, still with `Path=/`, no `Domain`, `HttpOnly`, `SameSite=Strict`, `Max-Age=900`, and the same expiry, but `Secure=false`. Production mode, a non-loopback HTTP public URL, or a mismatched mode/URL fails configuration validation rather than silently dropping `Secure` or changing cookie policy.
- Cookie clearing, when later needed, must use the same environment-selected name and attributes with `Max-Age=-1` and an expired date. Neither cookie is accepted outside setup-session middleware.

### Origin, CSRF, and proxy boundary

- Exchange and every subsequent setup mutation require exactly one syntactically valid `Origin` header whose normalized origin (lowercase scheme/host, effective default port, no path/query/fragment/userinfo) exactly equals the normalized origin of `REQUESTARR_PUBLIC_URL`. Missing, opaque/null, malformed, or mismatched origins return `setup_origin_forbidden`. This requirement applies to HTTPS and the local HTTP exception. No `Referer` fallback and no wildcard/subdomain allowance exists.
- Session-authenticated setup mutations additionally require exactly one `X-Requestarr-CSRF: 1` header. The fixed header is not a secret; its purpose is to force a non-simple request, while exact-origin validation and `SameSite=Strict` provide the authorization-bound controls. Richer setup `GET`/`HEAD` reads require a valid session but not the CSRF header. CORS is not enabled for setup routes, and preflight `OPTIONS` is rejected with the normal method contract.
- Requestarr trusts the server's direct connection peer only. It ignores `Forwarded`, `X-Forwarded-For`, `X-Real-IP`, and forwarded scheme/host for origin, cookie, and rate-limit decisions. A reverse proxy must preserve the configured public origin, provide TLS externally for production, and apply its own abuse controls; until an explicit trusted-proxy CIDR design exists, Requestarr may see all proxied callers as one rate-limit source. Host-header values never define the trusted origin.

### Durable rate limiting

- Token exchange uses PostgreSQL-backed fixed windows so restart and multiple instances do not reset enforcement. Two counters are updated for each well-formed, same-origin exchange with an invalid token: a per-peer counter keyed by the direct peer IP and a global counter. The peer key is `HMAC-SHA-256(setup_key, "requestarr/setup/v1/rate/peer\x00" || canonical_ip_bytes)`; the global key is `HMAC-SHA-256(setup_key, "requestarr/setup/v1/rate/global")`. No plaintext IP is stored.
- Windows are aligned 15-minute UTC intervals derived from PostgreSQL `transaction_timestamp()`. The limits are 5 failed candidates per peer per window and 100 failed candidates globally per window. Once a counter has reached its limit, further otherwise-processable exchanges for that key return HTTP 429 without token verification or session creation. The response includes `Retry-After` as whole seconds, rounded up to the next window boundary and clamped to 1–900.
- Counter increments use one atomic PostgreSQL upsert per key in a transaction, so concurrent instances cannot lose failures. The global and peer checks/increments occur in the same transaction; any database failure rolls back both and fails closed as `setup_unavailable`. Malformed, wrong-origin, locked, and completed requests do not consume token-failure counters. A successful exchange deletes the current peer window row but does not clear the global counter.
- Expired rate rows and expired/revoked session rows are security-inert. Opportunistic cleanup runs after a successful exchange at most once per process minute, uses bounded deletes (maximum 1,000 rows per table per pass), and removes rate windows older than 24 hours plus sessions expired or revoked more than 24 hours ago. Cleanup failure is logged by category without failing an already committed exchange. Correctness never depends on cleanup.

### Schema: append-only migration `000002`

- Keep `000001_initial` immutable. `000002_setup_bootstrap_sessions` is one transactional append-only migration that creates:
  - `setup_sessions`: `id UUID PRIMARY KEY`; `token_digest BYTEA NOT NULL UNIQUE CHECK (octet_length(token_digest)=32)`; `created_at TIMESTAMPTZ NOT NULL`; `expires_at TIMESTAMPTZ NOT NULL`; `revoked_at TIMESTAMPTZ`; checks that `expires_at > created_at` and `revoked_at IS NULL OR revoked_at >= created_at`; and indexes on `expires_at` plus unrevoked expiry (`expires_at WHERE revoked_at IS NULL`).
  - `setup_exchange_rate_limits`: `key_digest BYTEA NOT NULL CHECK (octet_length(key_digest)=32)`; `scope TEXT NOT NULL CHECK (scope IN ('peer','global'))`; `window_start TIMESTAMPTZ NOT NULL`; `failure_count INTEGER NOT NULL CHECK (failure_count >= 1)`; `updated_at TIMESTAMPTZ NOT NULL`; primary key `(scope, key_digest, window_start)`; and an index on `window_start` for cleanup.
  - `setup_security_events`: `id UUID PRIMARY KEY`; `event_type TEXT NOT NULL CHECK (event_type IN ('session_issued','session_revoked','sessions_revoked_on_completion','rate_limit_activated','setup_completed'))`; nullable `session_id UUID REFERENCES setup_sessions(id) ON DELETE SET NULL`; `data JSONB NOT NULL DEFAULT '{}'::jsonb`; `created_at TIMESTAMPTZ NOT NULL`; and an index on `(created_at DESC)`.
- The migration adds no raw token/session/IP column, no bootstrap-token table, and no reset flag. Its down migration exists for explicit local/test recovery only and drops these three new tables in dependency order; production remains forward-only.

### Endpoint and error contract

- `POST /api/v1/setup/session` is the only new route in the endpoint increment. Success is HTTP 204 with no body and the setup cookie. It always includes `Cache-Control: no-store`, request correlation, and existing security headers. `HEAD`, `GET`, `PUT`, `PATCH`, `DELETE`, and `OPTIONS` return HTTP 405 with `Allow: POST` and the standard API error envelope.
- Errors use the existing JSON envelope and exact stable outcomes below; no error sets a session cookie:

| Outcome | HTTP | Code | Message / required header |
| --- | ---: | --- | --- |
| Missing or malformed deployment token while incomplete | 423 | `setup_locked` | `Setup is locked by server configuration.` |
| Authoritative setup state is complete | 409 | `setup_completed` | `Setup has already been completed.` |
| Candidate is accepted-format but does not verify | 401 | `setup_token_invalid` | `The setup token is invalid.` |
| Peer or global limit is already exhausted | 429 | `setup_rate_limited` | `Too many setup attempts. Try again later.` and `Retry-After` |
| Missing/malformed/mismatched `Origin` | 403 | `setup_origin_forbidden` | `The request origin is not allowed.` |
| Wrong/multiple content type, empty/malformed/oversized/multiple JSON, unknown field, missing/non-string token, or token not in the accepted format | 400 | `invalid_request` | `The request body is invalid.` |
| Setup state, transaction, random source, session persistence, rate-limit persistence, or required database operation unavailable/cancelled | 503 | `setup_unavailable` | `Setup is temporarily unavailable.` |

- Processing precedence is: method; origin; content type/body/format; configured-token lock; one transaction containing setup-state lock, completion check, rate-limit checks, verification result persistence, session insert, and audit insert. Thus malformed input does not disclose lock/completion, while a valid-shaped request receives the authoritative locked/completed result. Database unavailability after parsing returns 503 rather than guessing.
- Future richer setup reads return 401 `setup_session_required` for a missing, malformed, unknown, expired, revoked, or key-rotation-invalidated cookie; they do not distinguish those states. Future mutations apply origin and CSRF checks first, then session authorization, and return 403 `setup_csrf_forbidden` for a missing/invalid CSRF header, 409 `setup_completed` after completion, and 503 `setup_unavailable` for persistence failure. Every mutation remains locked after completion even if a previously issued cookie is presented.

### Transactions, audit, and redaction

- Exchange uses one transaction and PostgreSQL time. It locks the singleton `setup_state` row `FOR UPDATE`; evaluates completion; atomically checks/updates invalid-attempt counters; and, on success, inserts the session and `session_issued` event before commit. Invalid-token counter updates commit with the 401 outcome. A rate-limit transition inserts one `rate_limit_activated` event per scope/key/window; repeated 429 responses do not create events. No session cookie is written until commit succeeds.
- Setup completion later uses one transaction that locks `setup_state`, validates the resumable draft and all intended configuration writes, persists those writes, sets `complete=TRUE`, revokes all live setup sessions, and inserts `sessions_revoked_on_completion` and `setup_completed` events before commit. Any failure rolls back the whole completion. External validation/network calls occur before, never inside, this transaction; completion revalidates persisted prerequisites under the lock.
- Audit event data is an allowlist of non-secret fields such as reason category, rate-limit scope, affected-session count, and request ID. It never includes raw or digested tokens, raw or digested sessions, cookies, CSRF headers, Authorization values, IP addresses or rate-key digests, request bodies, configuration secrets, or deployment environment values. Session issue audit may reference only the database session UUID.
- Access and application logs record bounded route patterns, status, request ID, event category, and duration only. They must not record request/response bodies for these routes, `Set-Cookie`, `Cookie`, `Origin`, `Authorization`, `X-Requestarr-CSRF`, token/session values or digests, rate key/digest, full client addresses, or errors wrapping secret-bearing input. Database/crypto failures are translated to stable categories before crossing adapter boundaries.

### Recovery/reset non-goals and consequences

- This contract adds no reset API, CLI reset command, magic link, local password, alternate bootstrap credential, token retrieval, database repair, automatic unlock, or post-completion reuse. Losing the token before completion requires an operator to set a new conforming environment token and restart; that rotation invalidates existing sessions but preserves resumable setup state. Recovery after completion remains a separately threat-modeled administrative design and must not be approximated by changing the environment token or editing `setup_state` manually.
- Durable database sessions and limits preserve restart and multi-instance correctness. The design uses only the Go standard library and PostgreSQL beyond existing dependencies, and keeps HTTP parsing/cookies in `internal/httpapi`, setup policy in `internal/setup`, concrete SQL in `internal/store/postgres`, typed secret configuration in `internal/config`, and construction in `internal/app`.

## D016 — Encryption-key lifecycle and encrypted secret storage (2026-08-15)

**Status:** Accepted for the encrypted secret-storage foundation.

- `REQUESTARR_ENCRYPTION_KEY` is exactly 32 cryptographically random bytes encoded as canonical unpadded base64url. It is independent deployment material and must never equal or be derived from `REQUESTARR_SETUP_TOKEN`. Exact equality of two valid decoded values is rejected, but derivation cannot be programmatically detected; operators remain responsible for generating independent random values.
- V1 uses AES-256-GCM with a fresh random 12-byte nonce for every write. The persisted envelope has version 1, a 16-byte domain-separated non-secret key ID, the nonce, and bounded ciphertext. The key ID is a truncated SHA-256 digest over `requestarr/encryption/v1/key-id\0` and the key; it identifies compatibility but is not a verifier suitable for authenticating a candidate key.
- Associated data binds the envelope version, length-delimited namespace, owner UUID, and length-delimited secret name. Copying an envelope to another semantic context therefore cannot decrypt. One active V1 key is supported.
- Missing or malformed key material is represented only as a non-secret state. An empty installation may start locked for future secret operations. A usable key atomically establishes or verifies the singleton active-key registry during startup. Once the registry or encrypted rows exist, startup fails before listen when the key is missing, malformed, mismatched, or persistence is inconsistent. The registry foreign key and every upsert enforce the active key ID at the database statement boundary; startup does not claim to decrypt every row.
- V1 has no online multi-key rotation. Rotation requires decrypting and atomically replacing every envelope while the old key remains available; until that workflow exists operators must not replace the key. Key loss is fail-closed and ciphertext is unrecoverable without a matching backup of the key.
- Database backups and encryption-key backups are one recovery unit. Store the key separately from PostgreSQL with equivalent access control, test restoration together, and never place it in database dumps, logs, support bundles, or source control.
- Threat model: encryption reduces disclosure from a database-only theft, read-only SQL access, and accidental dump exposure. It does not protect against a compromised running process, host/root access, environment-secret access, memory inspection, or an attacker controlling both PostgreSQL and the deployment key.
- Errors, configuration rendering, and logs expose neither key material nor plaintext. Future APIs return presence/status only and never stored secret values. Setup drafts must not persist plaintext; final activation may persist encrypted envelopes transactionally. Draft validation and final activation remain distinct states.

## D017 — Encrypted Authentication setup draft (2026-08-16)

**Status:** Accepted for the Authentication setup draft vertical slice.

- Authentication draft saves encrypt the OIDC client secret immediately. `client_type` is a required non-secret enum: `public` or `confidential`. Public clients must use `clear`, have no stored secret, and transition atomically by deleting any prior confidential secret. Confidential clients require either `replace`, or `preserve` only when an envelope already exists; they may never clear. Preserve, replace, and clear are distinct operations, and omission never implies deletion.
- The fixed singleton envelope context is namespace `setup.authentication`, owner UUID `00000000-0000-0000-0000-000000000001`, and name `client_secret`. APIs, workflow drafts, events, logs, and results expose only `client_secret_configured`; they never expose plaintext or store secret-presence metadata in the draft.
- Replacing with the same plaintext decrypts the existing envelope and compares in constant time. Identical non-secret and secret state is a no-op: no re-encryption, state timestamp update, or event.
- Empty drafts and canonical V1 General-only drafts remain readable. Writes use canonical V2 aggregate drafts with optional General and Authentication non-secret sections. General and Authentication saves preserve the other section. Unknown, malformed, noncanonical, secret-bearing, and unsupported-version drafts fail closed.
- Issuer processing is local normalization and validation only. Normalization lowercases scheme/host and removes one trailing path slash while preserving decoded `Path` and valid `RawPath` escaping. Encoded slash or backslash and literal/encoded dot segments are rejected as ambiguous; valid escaped Unicode, spaces, and non-separator percent encodings remain escaped. This increment performs no OIDC discovery, provider validation, test login, activation, completion, or external call.
- Authentication save locks the setup singleton, checks completion, validates the session after acquiring the lock, validates aggregate state, applies the envelope operation, advances monotonically to Providers, and commits exactly one `authentication_saved` event with all effects in one transaction. Missing or unusable encryption capability required by an operation returns the stable locked outcome.
- This rollout stores the draft secret before activation and supports one active encryption key. It explicitly chooses public/confidential setup semantics but adds no broader token-endpoint authentication method negotiation, online key rotation, post-completion editing or recovery, discovery metadata, or runtime OIDC behavior.

## D018 — Revision-bound OIDC discovery validation (2026-08-16)

**Status:** Accepted for the OIDC discovery-validation vertical slice.

- Discovery is an explicit setup action, never a startup operation or routine setup-state read. The action uses one locally derived `GET` to `<issuer-path>/.well-known/openid-configuration`; it does not accept a caller-supplied discovery URL, follow redirects, use an environment proxy, send cookies or authorization, or perform another network request. No database transaction remains open during DNS, dialing, TLS, response reading, or metadata validation.
- Production issuers require HTTPS. Development permits HTTP only for `localhost` or a literal loopback address. Issuer URL parsing continues to reject credentials, query, fragment, opaque form, ambiguous escaped separators, and dot segments. Discovery metadata must return an issuer string exactly equal to the saved normalized issuer; URL-equivalent but textually different values fail.
- Destination policy is deny-by-default against the reviewed IANA IPv4 and IPv6 special-purpose registries, including mapped IPv4, discard-only, benchmarking, documentation, protocol-assignment, translation, reserved/future-use, multicast, unspecified, loopback, private, link-local, CGNAT, and zone-bearing addresses. The narrowly listed globally reachable IANA exceptions remain public. An exact operator-configured `REQUESTARR_OIDC_DESTINATION_CIDRS` prefix may override special-use denial for HTTPS; development HTTP remains limited to loopback and cannot be broadened by this setting. The setting is a strict canonical CIDR list, not a permissive allow-private Boolean. Mixed allowed/disallowed DNS answer sets fail as a unit. All accepted answers are retained and dialing is pinned to those checked IPs while preserving the original hostname for HTTP Host and TLS SNI, closing DNS-rebinding and check-to-use gaps.
- The dedicated transport disables proxying and redirects, requires TLS 1.2 or newer, identifies Requestarr with a fixed User-Agent, and has bounded DNS, connect, TLS-handshake, response-header, overall-operation, response-size, field, and array limits. Cancellation propagates through every phase. Discovery accepts only a successful `application/json` response no larger than 256 KiB and rejects duplicate JSON object names, unknown trailing documents, and malformed or oversized metadata.
- Required metadata is an exact issuer; structurally safe absolute authorization, token, and JWKS HTTPS URLs (with only the development loopback HTTP exception); `code` response type; `public` or `pairwise` subject support; `RS256` ID-token signing; and `S256` PKCE. Endpoint structure rejects userinfo, fragments, zones, invalid ports, trailing-dot hosts, ambiguous path separators, and dot segments. Endpoint hostnames and HTTPS literals, including private or mixed-answer names, are intentionally not resolved during discovery because this slice does not call those endpoints and cached DNS approval would go stale. Evidence records only capabilities and a metadata fingerprint; it is not network trust. Every endpoint use must freshly resolve all answers, apply this same deny-by-default/allowlist policy, and pin dialing to the approved answers while retaining Host and TLS SNI. Public setup clients strictly require an explicitly advertised token endpoint authentication capability `none`; confidential setup clients strictly require explicit `client_secret_basic` (an omitted field is not defaulted). Discovery receives only issuer and client type. It never receives, decrypts, or transmits client ID, client secret, scopes, claims, mappings, setup token, session cookie, or other setup values.
- Every effective Authentication save atomically increments a monotonic `authentication_revision` and removes prior successful validation evidence. A true normalized no-op, including an unchanged secret, preserves both. Existing canonical Authentication V2 drafts backfill to revision 1; all other setup rows backfill to 0. Successful evidence is singleton, bounded, policy-versioned, and keyed to the exact current revision. Protected setup state exposes the revision and only evidence matching that revision and current policy, using local database reads without decryption or networking.
- Validation reserves durable aligned five-minute counters in a short authorized transaction before network work: at most 5 attempts per setup session and 50 globally. A session-overlimit attempt records the rate-limited audit event but does not reserve or increment global capacity. Rate state contains only keyed session scope or a fixed global scope and survives restart/multiple instances. `Retry-After` is bounded to the next window. Safe allowlisted workflow events record validation reservation/success/failure categories without URLs, addresses, metadata, credentials, session material/digests, request bodies, or provider error text.
- Orchestration uses three phases: a short transaction locks setup state, checks completion and session, verifies the requested positive revision, reserves both limits, and returns a narrow secret-free snapshot; discovery runs without a transaction; then a short transaction rechecks completion, session, revision, and canonical Authentication snapshot before committing evidence. A late result is stale and cannot persist. Equivalent concurrent successful evidence is idempotent; differing evidence for one revision/policy fails closed as unavailable rather than overwriting.
- Client-secret presence and token-endpoint authentication capability are separate. Discovery proves advertised compatibility only; it does not prove credentials, perform token exchange, persist PKCE/state/nonce, create a user/session, complete setup, or activate runtime OIDC. Materially incompatible provider behavior must be recorded as a decision conflict rather than silently weakening this policy.

## D019 — Audiobooks are V1; ebooks are V2 (2026-08-17)
Audiobooks join the required V1 media types. Audible product URLs are the initial link-first entry path; Audnexus is the preferred metadata/enrichment adapter, and Audiobookshelf is the authoritative V1 availability source. Ebooks are explicitly deferred to V2 until canonical work/edition metadata and inventory matching are designed; BookOrbit/Kavita/Calibre-family sources are research candidates only.

## D020 — Audiobook identity is edition-aware and provider-neutral (2026-08-17)
Prefer ASIN + Audible storefront region when present and retain authors, narrators, series/position, runtime, language, publisher, and alternate IDs as normalized metadata. Narrator can distinguish materially different audiobook editions. Existing records render and reconcile from local state even if Audnexus is unavailable. Provider adapters remain replaceable.

## D021 — Audiobookshelf is the sole V1 audiobook availability authority (2026-08-17)
Do not infer audiobook availability from Plex in V1. Synchronize Audiobookshelf into normalized inventory, prefer exact stable external-ID matches, and preserve last-known inventory on failed/incomplete synchronization exactly like other authoritative sources.

## D022 — Seerr + Libreseerr define the long-term discovery UX direction (2026-08-17)
V1 remains link-first, but the universal resolved-item/request modal and catalog presentation must be reusable by future native search, discovery rails, similar-item links, and entity pages. Seerr is the primary cross-media UX reference; Libreseerr is the primary reference demonstrating Seerr-like patterns for book/audiobook presentation. Discovery must emphasize simple local state and must not import acquisition-specific controls/statuses.

## D023 — Chaptarr is a domain/matching reference, not a runtime dependency (2026-08-19)
Use Chaptarr to inform audiobook edition identity, narrator/publisher significance, modular metadata, and reconciliation/matching behavior. Do not integrate Chaptarr into V1 availability and do not import its acquisition/indexer/quality-profile/root-folder concerns. Audiobookshelf remains the sole V1 audiobook availability authority.

## D024 — Reconciliation records match provenance and confidence (2026-08-19)
Every availability association must retain a bounded match method, confidence (`exact`, `high`, or `possible`), and safe evidence. `exact` and `high` may drive automatic `Available` transitions. `possible` matches are review-only and must never silently fulfill a request. The concept is cross-media, not audiobook-specific.

## D025 — Audiobook manifestations preserve edition-significant metadata (2026-08-19)
In addition to ASIN + storefront region, preserve narrator(s), publisher, language, runtime, release date, abridged state, edition/presentation type when known, and alternate stable identifiers. A different narrator or dramatized/full-cast production may be a materially different audiobook manifestation even when title and author match.

## D026 — Work-to-manifestation compatibility is reserved for V2 without expanding V1 scope (2026-08-19)
V1 continues to store/request audiobooks directly. Domain and persistence choices must not assume that an audiobook catalog item is the only possible representation of a written work. Ebook V2 may introduce a canonical work relation linking ebook and audiobook manifestations. Do not create a premature ebook/work hierarchy or require multi-provider consensus in V1.
