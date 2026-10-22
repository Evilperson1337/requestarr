# Implementation plan: scaffold to usable Requestarr

Date: 2026-10-20. Status: partial implementation delivered; M1 and V1 are not complete.

### Current delivered status (2026-10-20)

The implementation has moved well beyond the scaffold baseline below, but it does not yet satisfy the full M1 acceptance demonstration or complete V1. This snapshot describes shipped repository behavior, not the historical target architecture or future work in later sections.

| Milestone area | Delivered status |
| --- | --- |
| M1 — identity and request loop | OIDC authorization-code login/session handling and one-time admin bootstrap are implemented. TMDB, MusicBrainz, and IGDB canonical resolution feed request creation and the shared request API/UI; authenticated users can subscribe, and admins can approve/deny requests. These are useful working slices, not the complete secure configuration wizard and real-browser OIDC acceptance described below. |
| M2 — availability | Embedded SPA serving and a Jellyfin movie/TV sync path with positive-only matching and admin-visible sync status exist. Availability is not inferred from missing inventory; incomplete or failed syncs do not establish absence. RomM, Navidrome, Plex, and other library adapters are absent. |
| M3 — notifications | Durable in-app notifications and a global Apprise outbox/worker are implemented. Global Apprise delivery is at-least-once and can duplicate after an ambiguous upstream acceptance. |
| M4 / V1 hardening | Not complete. Setup UI is bootstrap-aware but does not edit configuration. Admin user listing and role changes, transactional role audit, last-admin protection shared with `ClaimAdmin`, and session revocation on role changes are implemented. New OIDC users default to viewer; repeat logins preserve existing roles without backfilling/changing them. Notification preferences, request/inbox pagination and filtering remain deficient or absent. Broader operational, accessibility, security, and acceptance work remains. |

**Remaining acceptance gaps:** general configuration and setup are environment-only; the browser setup page is a bootstrap/admin-claim flow, not a wizard for editing server configuration. Only the Jellyfin source and positive-only availability matching exist; missing availability is not inferred. RomM, Navidrome, Plex, and other adapters remain absent. Global Apprise is at-least-once. Notification preferences and request/inbox pagination/filtering remain deficient or absent. No live Keycloak, metadata-provider, library, or Apprise acceptance run is claimed, and no UI component testing is in place.

**Verified checks (2026-10-20):** migrations through `000012` are present; the full integration suite was reported passing against live PostgreSQL 17, but a 2026-10-21 rerun fails two Jellyfin tests (see [integration baseline](research/INTEGRATION_BASELINE.md)). `make fmt`, `make test`, `make vet`, `make web-check`, `make build`, and `git diff --check` passed. A Docker build succeeded with the prior full code snapshot before admin management was added; it has not been rerun for the latest code, so do not treat it as current-image verification. This evidence does not establish live Keycloak, metadata-provider, library, or Apprise acceptance. Real OIDC browser integration and end-to-end tests remain outstanding. Do not describe this repository as V1-complete.

### Historical scaffold baseline (2026-10-15)

The following inventory and risk notes are retained as a record of the plan's original baseline. They are not claims about the current repository; use the dated delivered-status snapshot above for current implementation status.

## 1. Purpose and implementation order

Deliver real functionality in coherent, reviewable slices, not another round of interfaces without working adapters. The first substantial milestone (M1) is **secure setup → OIDC login → resolve a supported link → create one shared request → another user subscribes → admin reviews it**, through the actual browser UI and PostgreSQL. M1 supports movie, TV, album, song, and game metadata. Availability is explicitly unknown until library work lands. Durable audit events ship in M1; native notification presentation and external delivery follow in M3.

Implement M1 as six sequential handoffs below; each includes working behavior and tests. Do not call the product V1-complete at M1. M2 adds authoritative availability; M3 adds event fan-out, native notifications, and Apprise; M4 completes adapters, operations, accessibility, and release hardening. No acquisition tools or download automation enter any milestone.

This document is the only planned change in this planning task. All file names in proposed-change tables identify future work, not newly implemented components. Accepted decisions remain those in [the decision log](DECISIONS.md) and [agent guardrails](../AGENTS.md). If an implementer discovers a conflict, stop and propose a dated decision amendment rather than silently replacing behavior.

## 2. Historical scaffold architecture and baseline (2026-10-15)

This section records the state observed when this plan was written. Its component-level descriptions and problem list intentionally remain unchanged for historical context and must not be read as a current status report.

| Area | Evidence and actual behavior |
| --- | --- |
| Executable | [Command entrypoint](../cmd/requestarr/main.go) uses standard-library flags for version and migrate-only; loads environment config and starts the application. No CLI framework is needed. |
| Composition | [Application](../internal/app/app.go) opens PostgreSQL, applies migrations, starts an HTTP server, and shuts down with cancellation. It does not construct auth, providers, request services, or workers. |
| Configuration | [Config loader](../internal/config/config.go) loads environment, bind address, public URL, log level, and database URL only. OIDC/provider variables in [environment examples](../.env.example) are not implemented; [YAML configuration](../config/requestarr.example.yaml) is an example, not an actual loader. |
| Persistence | [Store](../internal/store/postgres/store.go), [pool](../internal/store/postgres/pool.go), and [migrator](../internal/store/postgres/migrate.go) implement startup, embedded migrations, readiness, and shutdown. [Initial schema](../db/migrations/000001_initial.up.sql) creates nine product tables, but no lifecycle repository exists. |
| HTTP | [Router](../internal/httpapi/router.go) serves health/readiness/version, a hard-coded incomplete setup response, and scaffold HTML. Other routes listed in [OpenAPI](../api/openapi.yaml) are documentation placeholders, not implemented 501 handlers; currently they fall through to 404. |
| Domain | [Media types](../internal/domain/media.go) and [request types](../internal/domain/request.go) exist. The platform validator checks Any exclusivity/nonempty explicit selection only; it does not verify IGDB membership. Availability is currently conflated with a lifecycle constant and timestamp. |
| Integration boundaries | [OIDC](../internal/auth/oidc/provider.go), [metadata](../internal/providers/metadata/provider.go), [inventory](../internal/inventory/source.go), and [scheduler](../internal/jobs/scheduler.go) are boundary sketches. [Reconciliation](../internal/reconcile/service.go) is comments only. [Apprise](../internal/notifications/apprise/client.go) always returns an unimplemented error. |
| UI | [Application](../web/src/App.tsx) renders Home only. [Request modal](../web/src/components/RequestModal.tsx), [Requests](../web/src/pages/RequestsPage.tsx), and [Setup](../web/src/pages/SetupWizardPage.tsx) are static descriptions. [Vite](../web/vite.config.ts) already proxies API/auth paths. No working browser session or product workflow exists. |
| Tests | Config, domain platform selection, router/probes, and database redaction have tests. [PostgreSQL integration tests](../internal/store/postgres/postgres_integration_test.go) are build-tagged and require a dedicated test database variable; they test initial migration and reopen, not product transactions. |
| Layout/dependencies | [Module](../go.mod) uses a placeholder module path and currently depends on pgx and golang-migrate. There is no public package tree; keep application code internal rather than inventing a reusable SDK. |

### Strengths

- Small modular monolith, explicit SQL, existing migration embedding and database readiness boundary.
- Canonical catalog uniqueness, unique request per catalog item, and composite subscription key already encode central product invariants.
- Domain types are largely independent of adapters; HTTP does not directly import PostgreSQL.
- Existing startup/shutdown, config validation, and redaction tests provide useful regression coverage.
- Clear product decisions and UI direction in [architecture](ARCHITECTURE.md), [integration contracts](INTEGRATIONS.md), [setup](SETUP_WIZARD.md), [UI](UI_UX.md), and [scope](V1_SCOPE.md).

### Problems and risks to address before feature exposure

- No authentication, authorization, sessions, CSRF, setup capability enforcement, encrypted secret storage, or persisted runtime settings.
- Users are keyed by subject alone; real OIDC identities must use exact issuer plus subject. Never merge accounts by email.
- No atomic request/subscription/event writes, replay handling, lifecycle concurrency controls, or delivery deduplication.
- Existing request status includes available; authoritative availability needs its own state and evidence, not an admin-editable status.
- Inventory change sets do not express complete-vs-partial enumeration, generations, or durable reconciliation scheduling.
- Setup status is fabricated; UI/API drafts could be mistaken for implemented contracts.
- Development issuer/browser reachability and exact callback origins must be tested. Container hostname and browser-visible hostname are not interchangeable OIDC issuers.

### Environment observed, not a validation claim

Read-only checks found Go 1.27.1-X:nodwarf5 on linux/amd64, Node 26.10.0, npm 12.2.0, Docker and psql commands, and installed frontend dependencies. The [module](../go.mod) requires Go 1.27.1. No service health, baseline test pass, container connectivity, or provider credentials were verified by those checks. Pre-existing working-tree edits include development/build/docs/module files; preserve them. The [Makefile](../Makefile) frontend check is TypeScript checking, not a frontend test runner or linter; web-install runs npm install, not npm ci. Some claims in the [manual testing guide](MANUAL_TESTING.md) need correction during implementation documentation work.

## 3. M1 acceptance demonstration (remaining target; not yet passed)

Use a clean dedicated PostgreSQL database, Keycloak, deterministic provider HTTP fixtures, and a browser. Also exercise at least one opt-in real Keycloak run; do not substitute fake authentication for the security acceptance gate.

1. An anonymous visitor sees only minimal setup-required state; cannot read a draft, save credentials, resolve links, list requests, or claim administrator privileges.
2. Operator supplies an out-of-band random setup token, receives a short-lived setup-only session, resumes a draft after restart, and configures/test-validates OIDC and enabled metadata providers. All wizard steps appear; no libraries and external notifications disabled are explicitly supported with warnings.
3. Test login binds the intended first administrator's verified issuer/subject to this setup session and draft revision. Finalization atomically activates settings, creates/binds that administrator, records audit, marks complete, and invalidates every bootstrap capability. It then requires a normal OIDC sign-in.
4. Normal user signs in using Authorization Code + PKCE. TMDB movie/TV, MusicBrainz release-group/recording, and IGDB game links each resolve to local canonical catalog records. Disabled media fail with an explicit capability error rather than fake metadata.
5. Requester creates a request; the same transaction creates its automatic subscription and audit event. Default approval requirement is true for every enabled type. Another user resolving the same item sees Already Requested and can explicitly choose Notify Me.
6. Concurrent creation by different users produces one request, retains the first creator/platform criteria, and never silently subscribes the losing user. No duplicate rows or votes. Requests are visible to every authenticated role; My Requests filters attribution only.
7. Admin approves/denies/cancels with optimistic concurrency and an audited reason. No actor can manually mark content available. Viewer cannot create or review; requester cannot administer.
8. Restart preserves setup, identities, requests, subscriptions, policy, and audit. Routine list/detail/me reads work when metadata servers are unavailable; availability says unknown, not missing or available.
9. Browser flows show usable loading/error/empty states, preserve poster/square artwork, and have keyboard-accessible modal and game choices sourced from IGDB.

Historical M1 target: no notifications or actual library availability, and a full setup wizard. Current implementation differs: durable in-app and global Apprise notifications exist, Jellyfin positive-only sync exists, and the setup UI is bootstrap-only. These delivered slices do not make the full M1 acceptance target pass.

## 4. Package boundaries and composition

Keep the existing modular monolith. Proposed application-service packages are auth, settings, setup, catalog, requests, inventory, reconcile, notifications, and jobs. Domain contains pure values, capabilities, transition rules, and matching decisions; it never imports HTTP, pgx, OIDC libraries, or providers. HTTP owns transport DTOs and maps them explicitly to domain/application inputs. Avoid using domain structs as accidental JSON contracts.

Each service defines its small repository/adapter contracts at its own consumer boundary. PostgreSQL implements those contracts using explicit SQL. Do not expand the empty [store package](../internal/store/store.go) into a generic repository framework. Constructors receive repositories, bounded HTTP clients, clock/ID generators where tests need them, and immutable validated settings snapshots. Only the [application composition root](../internal/app/app.go) imports concrete adapters and wires services. Command wiring stays in the [entrypoint](../cmd/requestarr/main.go).

Required semantic boundaries (method signatures are finalized with the first contract handoff):

| Consumer | Contract and guarantees |
| --- | --- |
| Auth service | Identity repository upserts exact issuer/subject; session repository persists hashed opaque tokens and expiries; OIDC adapter performs discovery, code exchange and full ID-token verification. Existing OIDC boundary must be extended/replaced, not treated as sufficient. |
| Settings/setup | Draft repository saves redacted settings plus encrypted secrets with revision checks; finalization repository executes activation/admin/audit/revocation in one database transaction. OIDC test proof is bound to draft revision/session and expires. |
| Catalog service | Registry recognizes/parses only supported links; resolvers map validated references to normalized metadata; catalog repository upserts canonical item/cache and lists persisted platform options. No request creation in resolvers. |
| Requests service | Repository exposes atomic create-or-existing, subscribe/unsubscribe, transition, and local read operations. Inputs include actor and validated policy snapshot; operations recheck current role/policy/version under transactional consistency. Return explicit created/existing and changed/no-change outcomes. |
| Availability service | Inventory repository publishes successful generations plus durable affected-key jobs atomically. Reconciler reads exact candidate keys/evidence and commits availability changes, events, and outbox entries atomically. |
| Notifications | Fan-out repository derives deduplicated recipients/deliveries from committed events; delivery transport sends outside database transactions. Native notifications are durable data, not calls to Apprise. |

Transactions remain in PostgreSQL semantic repository methods: do not leak a pgx transaction into domain code or build a generic unit-of-work abstraction. Pure services validate commands; repositories recheck race-sensitive facts inside their transaction. Lock order is settings/actor as needed, then request, then subscription/event/outbox; document actual SQL locking and retry only known retryable serialization/deadlock errors with a small bound. Never hold a transaction during external HTTP.

### File-by-file implementation map

Paths below are proposed additions unless marked existing. Test siblings and sanitized adapter fixture directories ship with each implementation, not in a distant testing phase.

| Scope | Files and responsibility |
| --- | --- |
| Core | Existing [media](../internal/domain/media.go) and [request](../internal/domain/request.go); add [identity](../internal/domain/identity.go), [policy](../internal/domain/policy.go), [availability](../internal/domain/availability.go), [events](../internal/domain/events.go): typed identities, capabilities, state machine, evidence-independent availability, event names. |
| Runtime settings/secrets | Add [settings model](../internal/settings/settings.go), [settings service](../internal/settings/service.go), [settings repository contracts](../internal/settings/repository.go), [secret encryption](../internal/secrets/cipher.go); extend existing [environment config](../internal/config/config.go) for deployment-only inputs. |
| Auth | Add [auth service](../internal/auth/service.go), [session model](../internal/auth/session.go), [auth repositories](../internal/auth/repository.go), [authorization](../internal/auth/roles.go), [OIDC client](../internal/auth/oidc/client.go); revise existing [OIDC boundary](../internal/auth/oidc/provider.go). |
| Setup | Extend existing [state](../internal/setup/state.go); add [service](../internal/setup/service.go), [repository](../internal/setup/repository.go), [validation](../internal/setup/validation.go). |
| Catalog/providers | Add [catalog service](../internal/catalog/service.go), [repository](../internal/catalog/repository.go), [URL parser](../internal/catalog/url.go), [registry](../internal/providers/metadata/registry.go), [TMDB client](../internal/providers/metadata/tmdb/client.go), [MusicBrainz client](../internal/providers/metadata/musicbrainz/client.go), [IGDB client](../internal/providers/metadata/igdb/client.go). Existing resolver boundary can remain adapter-facing; catalog consumes only the methods it needs. |
| Requests | Add [service](../internal/requests/service.go), [repository](../internal/requests/repository.go), [commands](../internal/requests/commands.go), [queries](../internal/requests/queries.go); domain transition rules stay outside SQL and HTTP. |
| PostgreSQL M1 | Add [settings](../internal/store/postgres/settings.go), [secrets](../internal/store/postgres/secrets.go), [auth](../internal/store/postgres/auth.go), [setup](../internal/store/postgres/setup.go), [catalog](../internal/store/postgres/catalog.go), [requests](../internal/store/postgres/requests.go), [audit](../internal/store/postgres/audit.go). Keep existing pool/migration/readiness behavior. |
| HTTP | Split existing [router](../internal/httpapi/router.go) into routing plus new [auth handlers](../internal/httpapi/auth.go), [setup handlers](../internal/httpapi/setup.go), [catalog handlers](../internal/httpapi/catalog.go), [request handlers](../internal/httpapi/requests.go), [settings handlers](../internal/httpapi/settings.go), [middleware](../internal/httpapi/middleware.go), [DTOs](../internal/httpapi/dto.go), [errors](../internal/httpapi/errors.go). |
| Outbound safety | Add [HTTP transport policy](../internal/outbound/client.go) for bounded responses/redirect policy and [endpoint validation](../internal/outbound/endpoint.go) for administrator-configured destinations. Provider-specific validation remains in each adapter. |
| Composition/contracts | Extend existing [app wiring](../internal/app/app.go), [entrypoint](../cmd/requestarr/main.go), [OpenAPI](../api/openapi.yaml), [environment example](../.env.example). Add no YAML parser in M1; document that the YAML example is not loaded. |
| UI | Extend existing [App](../web/src/App.tsx), [Home](../web/src/pages/HomePage.tsx), [Requests](../web/src/pages/RequestsPage.tsx), [Setup](../web/src/pages/SetupWizardPage.tsx), [Settings](../web/src/pages/SettingsPage.tsx), [modal](../web/src/components/RequestModal.tsx), [Artwork](../web/src/components/Artwork.tsx), [styles](../web/src/styles.css). Add [API client](../web/src/api/client.ts), [contract types](../web/src/api/types.ts), [session state](../web/src/auth/SessionProvider.tsx), [request detail](../web/src/pages/RequestDetailPage.tsx). |
| Production UI | Add [asset adapter](../internal/webui/handler.go); extend [build targets](../Makefile) and [container build](../Dockerfile) to serve compiled assets and SPA routes from the same origin. API/auth unknown paths must remain JSON/404, never SPA fallback. Do not break Go-only test builds by requiring unbuilt embedded assets. |

## 5. Security and setup contract

### Deployment configuration and secret handling

Environment remains authoritative for bind/public URL, database connection, setup token, encryption key material, and development-only transport exceptions. Persist user-editable instance, OIDC, provider, request-policy, library, and notification settings in PostgreSQL. Effective settings come from the active revision, not ambiguous environment overrides. Reject wizard public URL differing from deployment public URL and show the corrective deployment instruction. OIDC/provider variables currently only shown in examples must be removed or clearly marked unsupported in the implementation update, not silently granted precedence.

Require a random 32-byte deployment encryption key (proposed REQUESTARR_SECRET_KEY, base64), with a key identifier. Encrypt saved secrets using authenticated encryption with fresh nonces and setting/secret identifier as associated data. Store ciphertext, nonce, and key ID separately from redacted configuration; keep keys out of PostgreSQL and logs. Invalid/missing keys fail closed for setup/config writes; never fall back to plaintext. Browser GETs expose only configured booleans. PATCH semantics: omitted secret retains it; explicit clear uses a separate clear flag, never a fake masked password. Key backup/rotation procedure must precede production release; loss of key requires re-entering integration secrets, not bypassing encryption.

Validate public URL as a canonical absolute origin with no credentials/query/fragment/path prefix for M1. Production requires HTTPS; explicit development mode permits loopback HTTP only. Do not infer callback origin or HTTPS from arbitrary Host/X-Forwarded headers. Document trusted proxy deployment and do not add permissive CORS.

### Controlled bootstrap

- Require operator-provided REQUESTARR_SETUP_TOKEN with at least 256 bits of generated entropy; reject shipped placeholder and weak values. Never print it or put it in URLs/browser persistent storage. Missing token does not expose unprotected setup.
- Token exchange is rate-limited by bounded process limiter plus persisted failed-attempt/backoff state for bootstrap; compare its digest in constant time. A token grants setup capability only, never normal API access or general admin role.
- Exchange creates a hashed opaque server-side setup session: 30-minute idle, two-hour absolute lifetime, host-only HttpOnly SameSite=Lax cookie, Secure in production. Rotate on exchange; draft remains resumable with a new valid exchange after expiry. CSRF token and exact Origin required for all cookie-authenticated writes, including draft saves, validation, completion, and logout.
- Public setup status reveals complete/required only; protect draft, validation details, secrets, and configuration endpoints. Completed state rejects token exchange even if the deployment still contains the token. No HTTP reset route in M1; recovery is a documented explicit operator procedure, not an unauthenticated escape hatch.
- Draft saves use an expected revision. Changing OIDC/public URL/provider config invalidates relevant validation proofs. Network validation occurs outside transactions, then commits a result only if the draft revision is unchanged.
- Setup OIDC test uses a purpose-tagged flow and separate callback. Bind verified issuer/subject to the setup session and revision. Require explicit Review confirmation of that identity as first administrator; never make the first unrelated normal login admin.
- Completion locks singleton setup state and checks revision, current validation proofs, enabled-provider requirements, and test-login proof. In one transaction activate immutable settings revision/secrets, insert or bind initial admin by issuer/subject, append setup audit, mark complete, revoke all setup sessions/flow records. Retry with the same completion operation ID returns the original outcome, not a second admin. Browser performs a fresh ordinary login afterward.

### OIDC/session behavior

Use maintained standards libraries: proposed coreos/go-oidc/v3 and golang.org/x/oauth2, versions chosen and reviewed at implementation time. Do not hand-roll JWT verification. Authorization Code with PKCE S256, random single-use state and nonce, five-minute server-side flow record hashed state, and browser binding. Consume flows atomically and prevent callback replay. Verify signature, allowed signing algorithm, exact issuer, audience/client and authorized-party where applicable, expiry and nonce. Exchange only at endpoints from the validated configured issuer; never fetch token/JWKS endpoints from untrusted token fields.

Normal session is a 32-byte random opaque cookie with only a digest in PostgreSQL, 30-minute idle/eight-hour absolute expiry, rotation at login, server-side logout/revocation, CSRF protection and secure cookie attributes. No access/refresh/ID tokens in localStorage, URLs, or browser JSON. Do not retain upstream tokens unless a demonstrated feature needs them. Existing local sessions can read local data during issuer outages; fresh login fails safely. User role/disabled state is checked from local authoritative state, not a stale cookie claim.

Default unmapped authenticated user is viewer; group role precedence admin > requester > viewer; scopes and groups-claim path are independent settings. Initial admin binding is an explicit persisted override so an absent groups mapper cannot lock out setup; later group mapping updates must not silently demote the final admin. Persist and audit explicit role overrides; provide admin user role/disable endpoints in M1. Protect last active administrator from demotion/disable under a settings-level transaction lock. Group claims are refreshed at login; administrative demotion/disable revokes sessions immediately. Changing issuer never aliases old accounts by subject or email.

### External endpoint and input safety

Metadata input must be a string ≤2 KiB. Parse once; reject userinfo, non-HTTPS schemes, nonstandard ports, encoded path separators, unexpected path structure, control characters, oversized IDs, and deceptive host suffixes. Permit only exact supported hosts and explicitly listed aliases. Strip harmless tracking query/fragment only after validating the recognized reference; do not follow an arbitrary pasted link.

Provider clients construct requests against fixed API origins from parsed IDs/slugs, never fetch the pasted URL. Disable redirects by default; any documented redirect exception must revalidate origin and never forward secrets across origins. Use bounded context/client timeouts (10 seconds per attempt, 15 seconds total resolve budget), ≤2 MiB decoded metadata responses, bounded JSON nesting/arrays/strings, correct content types, and provider-identifying User-Agent/contact. Handle 404, 401, 429/Retry-After, timeout, malformed data, and 5xx distinctly without leaking upstream bodies. Bounded concurrency, rate limiting, and cache prevent resolver abuse.

OIDC, library, and Apprise endpoints are administrator-configured integrations, not metadata input. Require explicit configured host/port allowlists; private LAN destinations can be allowed per source by the administrator, but loopback/link-local/cloud metadata destinations are denied outside explicit development exceptions. Resolve/validate actual dial addresses to prevent DNS rebinding, revalidate redirects or disable them, and enforce TLS verification. Discovery endpoint destinations are constrained to the issuer origin unless an additional origin is explicitly approved. Do not introduce a generic URL proxy or arbitrary artwork fetcher. Allow artwork only from known provider CDN origins; use a restrictive image policy and safe external links.

## 6. Metadata, canonicalization, and request policy

### Supported references and canonical keys

| Provider | Accepted initial URL form | Canonical identity and behavior |
| --- | --- | --- |
| TMDB | HTTPS www.themoviedb.org/movie/{positive-id}[-slug] or /tv/{positive-id}[-slug]; explicitly allow bare themoviedb.org alias | provider tmdb, media movie/tv, decimal ID without leading zeros. Movie and TV ID namespaces remain separate. Canonical external URL rebuilt from the ID. |
| MusicBrainz | HTTPS musicbrainz.org/release-group/{UUID} or /recording/{UUID} | provider musicbrainz, album/release-group or song/recording, lowercase validated UUID. Do not equate release IDs with release groups or work IDs with recordings. Other forms return unsupported_url with supported examples. |
| IGDB | HTTPS www.igdb.com/games/{validated-slug}; explicitly allow bare igdb.com alias | Query fixed IGDB API for exact slug; provider igdb, media game, returned numeric game ID. Require one exact result, not fuzzy search. Platforms derive from that game's payload, keys igdb:{numeric-platform-id}. |

TMDB bearer read token; IGDB client ID/secret with bounded cached Twitch client-credentials token refresh; MusicBrainz explicit application/contact identity and conservative global one-request-per-second pacing, including multiple service instances via database-coordinated budget or a documented single active provider worker. Respect upstream rate guidance; API credentials never reach browser. Cover Art Archive is optional presentation enrichment with its own bounded fixed-origin client: missing artwork does not fail resolution or change music identity.

Resolve persists normalized catalog item and allowed platforms but never creates a request. Cache freshness target 24 hours; known stale results can return immediately with stale=true and enqueue refresh. Cold misses perform bounded resolution. Initial M1 may refresh only on explicit resolution rather than requiring a background scheduler; never promise a queued refresh before jobs exist. Local list/detail routes never call providers. Do not store arbitrary provider payload wholesale; allowlist bounded presentation metadata and external identifiers.

Request creation accepts only a server-issued catalog item UUID, not client-supplied title/provider IDs/artwork. Require enabled type, valid local metadata, and current platform membership. Game selection defaults explicitly to Any; Any forbids explicit keys; explicit keys are unique, nonempty, capped at 64, and members of the persisted IGDB list. Reject platform selection for non-games; normalize its stored empty criteria. Existing request criteria are immutable in M1: duplicate create neither overwrites platforms nor creates platform-specific rows. UI shows the existing criteria before Notify Me. Editing criteria later requires an explicit audited admin workflow, not automatic unioning.

M1 TV requests mean the whole series; no season input accepted. Music canonicalization remains release-group/recording; game edition/DLC acquisition workflows stay out of scope.

### Authorization and transitions

| Action | Viewer | Requester | Admin |
| --- | --- | --- | --- |
| Read shared catalog/requests and resolve enabled links | Yes | Yes | Yes |
| Subscribe/unsubscribe own manual subscription | Yes | Yes | Yes |
| Create request | No | Yes | Yes |
| Cancel own pending requested item | No | Yes | Yes |
| Approve/deny/cancel any item | No | No | Yes |
| Change settings/policy/users; view private admin note/audit | No | No | Yes |

Creator subscription cannot be removed in M1 (409 requester_subscription_required); manual subscriptions use idempotent PUT/DELETE. Denied/cancelled requests remain the one canonical row; a new create returns it. Reopening is admin-only and uses that row. This preserves history without introducing duplicate requests.

Lifecycle is requested, approved, denied, cancelled. Rules: requested → approved/denied/cancelled; approved → cancelled; denied/cancelled → requested only through explicit admin reopen. Creator cancellation is restricted to requested. Every transition carries expectedVersion; stale version or illegal transition gives 409. Repeating the same operation ID with identical payload returns its recorded outcome; reused operation ID with different payload gives 409. Auto-approval creates approved plus creation/auto-approval events atomically when policy permits; admins also follow policy rather than hidden bypasses. Denial/cancellation requires a bounded plain-text reason; admin-private note is never included in shared DTOs/events visible to non-admins.

Availability is a separate unknown/missing/available aggregate with observed time, stale flag, and evidence/source links. M1 is unknown. Display status may be available only when lifecycle approved and matching authoritative evidence exists; never mutate lifecycle to available. Denied/cancelled remain visibly closed even if catalog evidence exists. Admin approval is not evidence of media availability.

## 7. API contract for M1

All product paths use /api/v1; auth redirect paths use /auth. Same-origin JSON, UTF-8, camelCase fields, RFC3339 UTC timestamps, UUID local IDs, opaque cursors. Strict request decoding: one JSON object, reject unknown fields, bounded body (64 KiB generally; setup draft at most 128 KiB), bounded query values. Error envelope: error object with code, safe message, optional fieldErrors, and requestId. Never serialize raw errors, SQL, secrets, callback codes, tokens, or upstream payloads.

Response codes: 400 malformed/unsupported input; 401 absent/expired appropriate session; 403 capability/CSRF failure; 404 unknown resource; 409 revision/state conflict; 413 body too large; 422 field/provider validation failure; 429 rate limit; 502 invalid upstream response; 503 temporarily unavailable or setup_required. Set Cache-Control: no-store on session/setup/config and actor-specific responses. Anonymous API requests return JSON 401, not an HTML login redirect.

### Setup/auth/settings endpoints

| Endpoint | Authorization and schema |
| --- | --- |
| GET /setup/status | Public → complete:boolean, required:boolean. No draft/issuer/identity details. |
| POST /setup/session | Before setup only; token:string in body → expiresAt:timestamp, csrfToken:string and setup cookie. Invalid token returns generic 401; completed returns 409 setup_complete. |
| GET /setup/draft | Setup session → revision:integer, currentStep:enum, config:RedactedSettings, validations:ValidationSummary[], administratorProof:verified/expired/absent plus safe display name. |
| PUT /setup/draft | Setup+CSRF; expectedRevision:integer, currentStep:enum, config:SettingsInput → redacted draft with incremented revision. Secrets write-only. |
| POST /setup/validate | Setup+CSRF; expectedRevision:integer, target:oidc/provider identifier → ValidationSummary with target, revision, valid:boolean, checkedAt, expiresAt, safe errors/warnings. Test remote services outside transaction. |
| POST /setup/oidc-test | Setup+CSRF; expectedRevision → authorizationUrl for purpose-bound redirect. Callback GET /auth/setup/callback consumes flow and redirects only to the fixed setup review page. |
| POST /setup/complete | Setup+CSRF; expectedRevision, operationId:UUID, confirmAdministrator:boolean → complete:true. Requires proof and all enabled-provider checks; replays of the committed operation are success without privileged details, even though session was revoked. All other post-completion setup writes rejected. |
| GET /auth/login | Explicit browser navigation; bounded relative returnTo allowlisted to local UI routes. Creates flow, redirects to issuer. Reject absolute/protocol-relative redirect targets. |
| GET /auth/callback | Validates bound flow/code/token, sets normal cookie, redirects to saved local route. No tokens in response JSON or redirect URL. |
| GET /me | Normal session → user:{id,displayName,role}, capabilities:string[], csrfToken:string, settingsRevision:integer, enabledMediaTypes:enum[]. Never expose session/token credentials. |
| POST /auth/logout | Normal session+CSRF → 204 and revoke cookie/session; local logout does not depend on upstream service. |
| GET /settings | Admin → revision, RedactedSettings. |
| PATCH /settings | Admin+CSRF; expectedRevision, changed settings/secret inputs → redacted settings. Require validation for changed integrations; invalidate issuer-dependent sessions when activating a new issuer/client. Record audit and retain last-admin safeguard. |
| GET /users | Admin → paginated safe user identities, role source/override, disabled flag; no OIDC tokens. |
| PATCH /users/{id} | Admin+CSRF; expectedVersion, roleOverride:role or null, disabled:boolean → user. Last-admin protected; revoke sessions on disable/demotion. |

SettingsInput consists of general(instanceName ≤120 chars, timezone valid IANA, publicUrl matching deployment), oidc(issuer, clientId, write-only clientSecret, scopes with openid required, groupsClaim, exact groupRoleMap), providers(enabledMediaTypes, TMDB token, IGDB credentials, MusicBrainz contact), requestPolicy(notesEnabled, approvalRequired per enabled media type). Libraries is empty and Apprise disabled in M1. Later milestone versions add validated source/config records, not arbitrary opaque JSON. RedactedSettings replaces secrets with configured flags. Validation expiry is ten minutes for completion, revision-bound; completion must never trust a browser-supplied valid flag.

### Catalog/requests endpoints

| Endpoint | Request and response |
| --- | --- |
| POST /resolve | Normal session+CSRF; url:string → 200 ResolveResult. Only this explicit action may perform a cold external fetch. |
| GET /requests | Normal session; filters q ≤200 chars, mediaType, lifecycle, displayStatus, requestedBy UUID, mine:boolean, subscribed:boolean; sort newest/oldest/title; limit default 25 max 100, cursor opaque → items:RequestView[], nextCursor:string or null. mine means created by current user, not subscription. Stable tie-break ID; cursor bound to normalized filters/sort. |
| POST /requests | Requester/admin+CSRF; catalogItemId:UUID, note:string ≤2000, platforms:PlatformSelection for games only; Idempotency-Key UUID required → 201 RequestView on create, 200 existing RequestView on canonical conflict, with created:boolean envelope. Existing outcome never subscribes caller. |
| GET /requests/{id} | Any normal session → RequestView plus safe event timeline (private admin payload omitted); no provider calls. |
| PUT /requests/{id}/subscription | Normal session+CSRF → 200 subscription:{subscribed:true,source:requester/manual}. Emit subscription event only on actual insertion. |
| DELETE /requests/{id}/subscription | Normal session+CSRF → 204 for absent/manual deletion; 409 for creator subscription. Audit actual deletion only. |
| POST /requests/{id}/transition | Admin, or creator cancellation under rule above, +CSRF; expectedVersion:integer, action:approve/deny/cancel/reopen, reason:string ≤2000, operationId:UUID → RequestView. No available action. |

CatalogItemView: id, mediaType, provider, providerId, title, year nullable integer, artwork:{url nullable, kind poster/square}, externalUrl, metadataFetchedAt. PlatformSelection: any:boolean, platformKeys:string[]; platform option: key, providerId, name. ResolveResult: item:CatalogItemView, platforms:option[], stale:boolean, existingRequest:RequestView or null, availability:AvailabilityView. Do not expose raw provider payloads.

RequestView: id, version, item:CatalogItemView, lifecycle, displayStatus, requester:{id,displayName}, note nullable, platforms nullable, createdAt, updatedAt, reviewedAt nullable, availability:AvailabilityView, subscription:{subscribed:boolean,source nullable}, allowedActions:string[]. AvailabilityView: state:unknown/missing/available, stale:boolean, observedAt nullable, sources:[{id,name,kind,externalUrl nullable}]. M1 returns unknown, stale=false, observedAt=null, sources empty. Shared views omit admin note, private email, raw issuer claims, and subscriber counts/lists. Display status is derived, not another writeable field.

Keep [OpenAPI](../api/openapi.yaml) authoritative as each slice lands: remove scaffold responses only when handlers/tests exist. Define request/response/error/security/cookie/CSRF schemas there before UI integration. Generate TypeScript types only after semantics are reviewed, or maintain a small explicitly checked DTO file initially; never add a large generated server framework just to share types.

## 8. Persistence, audit, and transaction guarantees

### M1 migrations: append only

Do not edit [initial up migration](../db/migrations/000001_initial.up.sql) or [initial down migration](../db/migrations/000001_initial.down.sql). The numbers below reserve the next migrations if still unused when implementation starts; recheck the repository before allocation.

| Proposed migration pair | Changes and backfill |
| --- | --- |
| [000002 identity/settings up](../db/migrations/000002_identity_settings.up.sql) / [down](../db/migrations/000002_identity_settings.down.sql) | Add issuer-scoped identity uniqueness replacing subject-only uniqueness; disabled/role-override/version fields; settings revisions/active singleton; encrypted secret records; normal/setup sessions; OIDC flows; revisioned setup state; system audit events; bounded bootstrap attempt records. Existing users with unknown issuer are quarantined as disabled, not guessed or linked by email; require explicit operator mapping before NOT NULL enforcement. Empty scaffold databases upgrade automatically. |
| [000003 request contracts up](../db/migrations/000003_request_contracts.up.sql) / [down](../db/migrations/000003_request_contracts.down.sql) | Add request lifecycle, version, availability state/observation fields; populate lifecycle from existing status, mapping old available to approved while preserving old available timestamp as legacy unverified data and setting authority state unknown. Add persisted platform options, idempotency operations, event aggregate version/sequence, durable outbox, and supporting indexes. Retain old status column during expand/migrate window; new code writes a documented compatibility projection until a later removal migration. |

New settings/auth changes use system audit events, since existing request_events requires a request FK. Audit actor may be setup capability/system rather than a fabricated user. Request events include unique event ID, request ID, event type, aggregate version/sequence, nullable actor ID, safe versioned payload, UTC time, and operation correlation ID. Do not place tokens, integration secrets, private admin notes, or raw provider payloads in either audit stream.

Outbox rows reference committed events with unique consumer/event key and durable processing state, attempts, availableAt, lease owner/expiry. M1 produces outbox work but leaves notification consumer disabled; event retention is sufficient to process/replay later. M3 establishes recipient semantics from subscription event history at event sequence, preventing new subscribers receiving old events. Never infer an old event's audience solely from current subscriptions. Events for subscription changes are auditable but do not themselves trigger notification loops.

### Atomic write algorithms

- **Resolve:** validate/fetch outside transaction; upsert canonical catalog key and normalized platform options atomically, then load existing request/local availability. Resolve failure cannot create a partial request.
- **Create:** check idempotency row keyed by actor+route+key and canonical normalized payload hash; lock active settings/policy and actor consistently; insert request with unique catalog item key. Insert winner gets requester subscription plus request.created (and auto-approved if applicable), aggregate version, and outbox rows in the same transaction. On unique conflict, read committed winner, return existing without mutation/subscription. Record replay outcome atomically; concurrent same-key operations wait/return consistent result. A reused key with another body is 409. Retain keys at least 24 hours; canonical uniqueness remains permanent after expiry.
- **Transition:** lock request; validate role/policy/version/state; persist lifecycle/review fields/version, safe event, outbox, and operation result atomically. Any failure rolls back all of them. Repeated desired state is not permission to append a second event.
- **Subscribe:** lock request first, then insert/delete composite subscription key; creator invariant cannot be downgraded. Append event and outbox only if state changes. Concurrent subscribe/create/transition tests must prove correct event ordering.
- **Setup completion:** single transaction described above; no partial active configuration or bootstrap administrator if failure occurs.

Use database constraints for uniqueness/FKs/checks as the final guard, not only application prechecks. No external sends inside transactions. Local event persistence is exactly once per successful semantic change; downstream external delivery is at least once, not falsely claimed exactly once.

### Migration operations

Back up database and encryption keys first. Test fresh install, upgrade from migration 1 with representative legacy rows, repeat startup, transaction rollback, and constraints. Quarantine ambiguous identity/legacy availability rather than inventing an issuer/evidence. Document upgrade preflight output before adding restrictive constraints. Use expand → deploy/read new columns → later cleanup; old binaries must not be assumed compatible after new feature writes. Deployment rollback is forward-fix or tested backup restore, not automatic destructive down migrations. Test down migrations on disposable databases only and explicitly reject any lossy downgrade that cannot preserve new lifecycle/identity semantics.

Retain the migration advisory lock and existing shutdown regression tests. Review migration deadline behavior: the existing runner does not take a context, so use bounded lock/statement timeouts before claiming startup cancellation bounds. No destructive schema reset in application startup or ordinary test helpers.

## 9. M2: authoritative availability in working slices

M2a builds durable job execution, successful-generation publication, exact-ID reconciliation, and **one Jellyfin movie/TV adapter** end to end. M2b adds RomM with IGDB platform evidence; M2c adds Navidrome music and Plex, then Jellyfin music support. Each adapter must have real fixture-backed implementation plus an opt-in connection test, not another README-only boundary. Verify current upstream API/version contracts before coding; do not copy reference project code with incompatible licensing.

### Files and boundaries

- Extend [inventory boundary](../internal/inventory/source.go) to distinguish page/delta, authoritative completion, source generation and capability. Add [inventory service](../internal/inventory/service.go), [repository](../internal/inventory/repository.go), and real [Jellyfin](../internal/inventory/jellyfin/client.go), [RomM](../internal/inventory/romm/client.go), [Navidrome](../internal/inventory/navidrome/client.go), [Plex](../internal/inventory/plex/client.go) adapters.
- Replace in-memory-only job sketch in [scheduler](../internal/jobs/scheduler.go) with durable typed job kinds and bounded JSON payloads, not serialized executable objects; add [worker](../internal/jobs/worker.go), [job repository](../internal/jobs/repository.go), [PostgreSQL jobs](../internal/store/postgres/jobs.go).
- Implement [reconciler](../internal/reconcile/service.go), [matching rules](../internal/reconcile/match.go), [reconcile repository](../internal/reconcile/repository.go), [inventory SQL](../internal/store/postgres/inventory.go), [availability SQL](../internal/store/postgres/availability.go), [library handlers](../internal/httpapi/libraries.go).
- Next append-only migration adds sync runs/generations/staging items, source leases/fencing tokens/cursors, normalized external-identifier index, request-to-inventory match evidence, durable jobs, and per-source observation/health fields. Preserve existing normalized inventory table and migrate records as unverified until a successful source run.

### Data guarantees

Stream bounded pages to staging, never hold entire libraries in memory. Source lease with fencing token prevents a timed-out old worker publishing over a new one. Only a complete, successful authoritative enumeration may publish a generation, advance cursor, and remove prior items absent from that same source's new generation. A failed page, timeout, malformed payload, auth error, or aborted run cannot publish removals. Valid incremental tombstones apply only under a proven complete/valid source delta contract.

Publication atomically updates inventory/cursor/health and inserts durable jobs for affected canonical keys. Worker crash between publication and reconciliation cannot lose changes. Match exact external IDs first; title/year-only candidates are reviewable suggestions and cannot automatically declare availability in initial M2. Normalize MusicBrainz release-group vs release and recording vs track; do not pretend title equality is canonical identity. Game match requires IGDB game ID plus Any or at least one selected platform; unknown platform evidence cannot satisfy explicit criteria. Map RomM platform identifiers explicitly to stable IGDB keys, never display-name equality.

Reconcile only affected requests by indexed canonical/external IDs, plus newly created/approved requests against local inventory. Persist match evidence, availability state/observedAt, event and outbox transactionally. Status and availability remain separate. Request.available emits only for a real transition into satisfying evidence; repeat sync is a no-op. A failed source retains previously known evidence with stale=true, never mass-marks requests missing. Aggregate all enabled authoritative sources; removing one source item cannot erase availability still proven by another. Full successful removal from all relevant sources gives missing; incomplete coverage gives unknown/stale as appropriate. Disabling a source is an explicit audited configuration action with reviewable consequences, not a simulated successful empty sync.

### API/UI additions

Admin GET/POST /libraries, PATCH /libraries/{id}, POST /libraries/{id}/test, POST /libraries/{id}/sync, GET /jobs and GET /jobs/{id}; source writes use expectedVersion/operationId and existing CSRF rules. Library input: kind, name, baseUrl, enabled, authoritativeMediaTypes, write-only credentials; output contains configured flag, health, itemCount, lastSuccessAt, lastErrorCode, nextSyncAt. Validate media authority per adapter. Sync returns 202 with jobId and deduplicates overlapping source work. Add local GET /catalog/{id}/availability returning AvailabilityView. Source deep links must use an allowlisted configured source/provider origin, never arbitrary stored JavaScript/data URLs. UI shows observed/stale state, source health, and Open in Library without outbound calls on rendering.

## 10. M3: native and Apprise notifications

First ship native notifications and historical recipient fan-out, then Apprise stateful/config-key mode. Stateless arbitrary destination URLs are deferred.

- Extend [notification boundary](../internal/notifications/notifier.go); add [fan-out](../internal/notifications/service.go), [repository](../internal/notifications/repository.go), [delivery worker](../internal/notifications/worker.go), [PostgreSQL notification SQL](../internal/store/postgres/notifications.go), [HTTP handlers](../internal/httpapi/notifications.go). Replace existing [Apprise stub](../internal/notifications/apprise/client.go) with a tested bounded client against verified current API behavior.
- Append migration for native notification rows/readAt, user preferences, delivery recipient/config snapshot, retry schedule, lease/fencing, terminal failure/dead-letter fields, and unique event+user+channel. Existing delivery table alone cannot deduplicate or durably schedule retries.
- Fan-out consumes committed outbox events in deterministic request sequence. Recipient membership is evaluated at event sequence using M1 subscription change history (or a transactionally captured audience snapshot), not current-time membership. Apply privacy-conscious preferences; subscription changes do not notify themselves. Native record insertion and marking fan-out processed commit together with unique constraints. Do not flood users with every pre-M3 historical event: deployment establishes an explicit activation watermark; audit remains replayable for operators, automatic delivery starts at that watermark.
- Apprise calls happen after committing a claimed delivery lease. Bounded exponential backoff with jitter, Retry-After, timeout, max attempts, and a terminal failed state. Store only sanitized error codes. Crash after remote acceptance before sent acknowledgement may duplicate external delivery; disclose at-least-once semantics and use upstream idempotency support only if verified.
- Request-created/approved/denied/cancelled/available events have explicit templates and per-user preferences; creator automatically participates through subscription. No subscriber list, private admin note, secret URL/config key, or sensitive claim in shared payloads. Single instance gateway key does not imply broadcast: use configured recipient tags/routes with explicit validation. Never send one user's notification to every gateway destination accidentally.
- GET /notifications returns the current user's cursor-paginated native records; PATCH /notifications/{id} accepts read:boolean and expectedVersion, verifies ownership; GET/PATCH /notification-preferences manages own enabled event/channel preferences; admin POST /settings/notifications/test queues an audited test job for an explicit target. Setup enables Apprise only after successful bounded validation; routine pages remain local during outages.

## 11. Sequential specialist handoffs and dependencies

Each handoff ends with working code/tests, updated OpenAPI where relevant, and focused documentation. Do not delegate all work simultaneously: shared schema/contract/composition files require serial ownership. A receiving specialist reads this plan and current changes first; no implementation is authorized by this planning-only task until the next execution step.

| Order / specialist | Exact scope and dependencies | Acceptance / handoff artifact |
| --- | --- | --- |
| M1.1 — Go API + Data Persistence, sequential | Freeze M1 DTO/error/authorization contracts in [OpenAPI](../api/openapi.yaml); implement domain identity/policy/availability rules, settings/secrets repositories, migration 2, and deployment config validation. Use Go Security Reviewer for key/bootstrap design. No generic repository layer. | Fresh/upgrade migration tests, encrypted-secret round trips/tamper failures, redacted DTO tests, role/state unit tests. Publish exact constructor and repository signatures for M1.2–4. |
| M1.2 — Go API/auth implementer + Web UI | Implement setup/session/OIDC service and SQL, actual Keycloak login/callback/logout, setup handlers, complete resumable wizard, initial admin binding, settings/users access. Depends on M1.1. Extend dev realm/import only if needed; prove browser-visible issuer reachability. | Clean database → secure wizard → fresh normal login → /me; setup revocation and unauthorized/viewer/admin tests; CSRF/state/nonce/replay/last-admin tests. This is a working intermediate slice, not final M1. |
| M1.3 — Go API/provider implementer | Implement URL parser, registry, all three provider adapters, catalog SQL/cache, fixture suites, /resolve. Depends on M1.1 contracts and M1.2 authentication. Add initial modal URL resolution UI if Web UI owner available, serializing overlapping files. | Five media types resolve via fixtures; malformed/SSRF/429/timeout tests; IGDB dynamic platforms; no request mutation; disabled type and missing-artwork behavior. |
| M1.4 — Go Data Persistence + Go Implementer | Migration 3, requests service/SQL, idempotency, subscriptions, transitions, events/outbox, local queries and HTTP endpoints. Depends on M1.1–3. | Concurrent canonical create, rollback fault injection, correct creator subscription, existing-no-auto-subscribe, immutable criteria, policy/version race tests, local reads with external services down. |
| M1.5 — Go Web UI Engineer | Wire shared queue/detail, filters in URLs, modal branches, Notify Me, review controls, user access settings, session/CSRF/error client, and same-origin production assets. Depends on M1.2–4 stable API. Follow [UI contract](UI_UX.md) and [final mockups](../references/mockups/final/). | Browser demonstration from section 3; keyboard/accessibility/responsive behavior; durable state after refresh/restart; no fake availability or recommendation shelves. |
| M1.6 — Go Test Engineer → Security Reviewer → Code Reviewer | Add/finish browser and cross-layer regression suites, run full gates, review auth/session/bootstrap/SSRF/SQL boundaries. Documentation writer updates setup/config/manual checks after behavior stabilizes. | M1 acceptance transcript, non-skipped dedicated DB and real Keycloak checks, documented known limitations. Fix findings before M2. |
| M2a — Data Persistence → API/library → Implementer → Web UI | Durable jobs/generation publication, Jellyfin movie/TV, exact-ID reconcile and source controls. Depends on M1.6. | Simulated inventory arrival changes availability; partial sync/outage retains evidence; crash recovery and duplicate jobs do not duplicate availability events. |
| M2b/c — Integration specialists serially | RomM/platforms, then Navidrome/Plex/Jellyfin music; reuse tested inventory contract, add source-specific fixtures and migration only when needed. | All required source/media authority combinations; exact canonical identity and game OR tests; bounded pagination. |
| M3 — Data Persistence → Notifications/API → Web UI → Security | Recipient history/watermark/native feed, then real Apprise worker/preferences/test flow. Depends on M1 events and M2 availability; native status notifications can begin after M1 if coordinated. | Unique fan-out, privacy isolation, retry/lease/crash tests, upstream outage independent of UI, delivery semantics documented. |
| M4 — Maintenance/Performance/Security/Release | Operational status, recovery/backup/key rotation, remaining accessibility, targeted query/index checks, packaging and release evidence. Depends on M2/M3 complete. | Full V1 end-to-end matrix and product boundary audit; no release on skipped security/integration gates. |

### Dependency decisions

Keep pgx, golang-migrate, standard-library HTTP routing, flags, logging, and contexts. Add only reviewed OIDC/OAuth libraries and focused frontend browser-testing dependencies (proposed Playwright; component test tooling only if useful). Choose and pin versions during implementation, update lock/module sums, audit licenses/vulnerabilities. Prefer standard-library crypto for opaque tokens, UUID generation with tested RFC variant/version bits, authenticated encryption, and HTTP test servers; no JWT implementation or broad framework. No YAML loader, ORM, Redis, queue service, Cobra/Viper, or additional database is necessary. Decide the placeholder module path before a release, not as an unrelated rename mixed into the first security slice.

## 12. Test plan and commands

### Required matrix

- Pure units: URL fuzzing/normalization; roles and overrides; transition state machine; duplicate platform removal/membership; Any OR rules; canonical identity and artwork shape; policy defaults; cursor validation.
- Auth/security: anonymous/setup/normal session separation; viewer/requester/admin paths for every mutation; state/nonce/PKCE and callback replay; wrong issuer/audience/expired token/unsafe algorithm; session expiry/revocation; CSRF and Origin; open redirect; settings secret redaction; issuer collision; last-admin race; bootstrap brute-force/completion race; draft revision proof invalidation. Fake OIDC issuer for deterministic protocol failures plus opt-in real Keycloak browser tests.
- Provider fixtures: five media types; hostile URL/IP/port/path; oversized decoded responses, wrong type, missing IDs/artwork, ambiguous IGDB slug, token refresh concurrency, 404/401/429/Retry-After, cancellation, 5xx, redirect secret leakage, DNS rebinding and admin endpoint allowlist. No live provider required for ordinary CI.
- Database integration: fresh and migration-1 upgrade; constraints; concurrent creates and idempotency conflicts; creator subscription invariance; transition/version races; rollback after each planned write; no partial event/outbox; repeat startup and shutdown. Test against a disposable dedicated database only.
- M2: page failures never remove; completed snapshot removals are source-local; stale worker fencing; atomic generation/job publication; jobs survive crash; exact-ID candidate indexing; multiple sources; unknown platform; Any vs selected OR; request creation after inventory already exists; source disabled vs outage; no-op reconciliation emits no duplicate event.
- M3: event-time recipient selection, activation watermark, dedup per event/user/channel, owner-only read APIs, retry limits/Retry-After, lease expiry/fencing, crash after send, disabled user/preferences, no private note/secret leakage.
- Browser: real wizard and login, all five media types, shared queue and bookmarkable filters, duplicate/Notify Me, admin review, restart persistence, native notification ownership, simulated arrival/outage, artwork proportions, modal focus/keyboard and responsive layout. Run M1-only subset before later functionality exists.

### Commands

Existing required commands from [guardrails](../AGENTS.md) and [Makefile](../Makefile):

- **make fmt** — required for implementation changes; modifies Go files. For this planning-only task, do not run a source-rewriting formatter merely to edit documentation; inspect formatting read-only instead.
- **make test** and **make vet** — ordinary Go gates.
- **make web-check** — current frontend TypeScript gate, not browser tests.
- **make web-build** and **make build** — packaging smoke checks when product code/build wiring changes.
- **go test -race ./...** — race gate for new services/workers.
- **go test -tags=integration ./internal/store/postgres/... -count=1 -v** — existing integration mechanism. Requires REQUESTARR_TEST_DATABASE_URL pointing to a separately created disposable database. A skip is not a pass; CI must provision that database and fail if unavailable. Never reuse the application's development/production database or blindly run destructive migrations there.
- **cd web && npm ci** — reproducible dependency install when needed; not performed by this plan. Existing installed dependencies allow the current type check.
- **cd web && npx playwright test** — proposed after M1.5 adds pinned Playwright config, browser tests, and a dedicated service/test environment; not an existing command guaranteed to work today.

Before DB/browser gates, explicitly provision isolated test PostgreSQL and a Keycloak realm with viewer/requester/admin users and a fixed browser-reachable issuer/callback. Existing [development compose](../compose.dev.yaml) is a starting point, not an isolated destructive test environment; do not run down-with-volumes against a developer's stack. Deterministic mock provider/library/Apprise servers supply test behavior. Live external-service checks remain opt-in and never require real secrets in CI logs.

## 13. Risks, tradeoffs, and exit criteria

- M1 is intentionally substantial: all metadata families plus secure auth/setup/requests. Six small sequential slices control scope; do not try to deliver all of V1 in a single specialist turn. The first next implementation is M1.1, immediately followed by working M1.2, not another architecture-only pass.
- Separating lifecycle/availability corrects a scaffold modeling limitation while preserving the documented convenience display status. Migration must not invent trusted availability from legacy timestamps. Review any differing UI interpretation against accepted decisions before coding.
- Encrypted settings require durable deployment key management; this is real operational cost but necessary for setup-saved secrets. Do not hide it with a hard-coded key or ephemeral process key.
- Explicit initial-admin override improves controlled bootstrap but needs documented role provenance and last-admin protection. Default viewers avoid privilege escalation while requiring an intentional operator/group grant for request creation.
- Stale local data prioritizes routine UI reliability; show its age/health, and never mistake unknown for missing. Provider and inventory freshness are independent.
- Durable outbox/history costs more SQL than direct calls, but avoids losing audit/delivery work and makes later specialists independent. No distributed broker needed at household scale.
- Fixed API origins and explicit admin endpoint allowlists constrain interoperability intentionally; add narrowly tested exceptions rather than disabling SSRF controls globally.
- Shared canonical game criteria can differ from later users' preferences. M1 makes this visible and immutable, keeping the one-request rule; do not silently create platform-specific duplicates.
- External delivery cannot generally guarantee exactly once. Native rows and event fan-out can be deduplicated; Apprise may deliver twice after ambiguous network failures.

**M1 is done only when section 3 passes through the real UI/API/database with real OIDC, all required auth roles are tested, and the working request loop survives restart. V1 is done only after authoritative library reconciliation, native + Apprise notifications, full wizard/settings capabilities, and the final security/operational gates are complete.**
