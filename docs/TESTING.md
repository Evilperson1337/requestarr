# Testing Strategy

## Unit
- URL recognition and provider ID extraction.
- request state transitions;
- platform Any/multi-select validation;
- subscription creation/uniqueness;
- reconciliation by exact external ID;
- fallback match confidence;
- role/capability authorization.

## Contract/fixture
Store sanitized provider/library fixtures under `testdata/` and test mapping independently of live services.

## Integration
Use PostgreSQL plus dev-compose Keycloak. Add mock HTTP servers for TMDB/IGDB/MusicBrainz/Plex/Jellyfin/Navidrome/RomM/Apprise. Live integration tests must be opt-in and never required for ordinary CI.

### Running the PostgreSQL integration suite

```bash
make test-integration
```

This starts the dev-compose `postgres` service, creates a dedicated `requestarr_test` database (never the application's `requestarr` database), and runs `go test -tags integration ./internal/store/postgres/...`. Without the `integration` tag, or without `REQUESTARR_TEST_DATABASE_URL`, these tests are skipped.

- If host port 5432 is taken, use `make test-integration REQUESTARR_POSTGRES_PORT=5433`.
- To use your own disposable database, set `REQUESTARR_TEST_DATABASE_URL`; the target then skips compose and runs against it.
- `scripts/check.sh` runs the suite when `REQUESTARR_TEST_DATABASE_URL` is set, and skips it otherwise.

## End-to-end
Cover setup wizard, OIDC sign-in, create request, duplicate resolution, Notify Me, admin approval/denial, simulated inventory arrival, Available transition, notification delivery, source outage retaining last-known state.

## Performance targets (initial, validate later)
- shared Requests page backed by local DB should feel instantaneous at household-scale inventories;
- no external API call on ordinary Requests page rendering;
- resolution UI progressively shows cached data when possible;
- library sync memory use must be bounded/paginated.
# Scripted Keycloak login check

`make check-login` (`scripts/check-keycloak-login.sh`) proves OIDC login works
end to end against the dev compose Keycloak. It starts Postgres and Keycloak
(recreating Keycloak so the realm import is current), builds and runs Requestarr
on the host (not in compose) against a throwaway `requestarr_login_check` database, then uses
curl to start login, submit the realm's dev user credentials, complete the
callback, and confirm `/api/v1/auth/me` returns the signed-in user. It prints
`PASS: ...` and exits 0, or prints `FAIL: <step>` and exits non-zero. Requires
docker, curl and go.

- Honors `REQUESTARR_POSTGRES_PORT` (use it when 5432 is taken, as for
  `make test-integration`), `REQUESTARR_LOGIN_CHECK_PORT` (default 18080) and
  `REQUESTARR_KEYCLOAK_URL` (default `http://localhost:8081`).
- It needs the development-only allowance for a plain-HTTP loopback issuer; any
  other issuer must be HTTPS.

Last recorded result: 2026-10-24, PASS against Keycloak 26.3
(`REQUESTARR_POSTGRES_PORT=5433 make check-login`). The first run exposed that
first sign-in failed with HTTP 500 because the callback used a non-UUID user ID;
that is fixed and covered by `TestCallbackProvisionsUserWithUUID`.

# Focused Requests browser tests

The Requests page has a Playwright suite with a deterministic mocked API. It
does not require PostgreSQL, a live identity provider, or library credentials.
Playwright is a development-only dependency; no browser framework is shipped
with the app. The suite runs in CI (`web` job) after the typecheck/build.

Coverage by spec file: `smoke.spec.ts` (Home resolve, request modal incl. game
platform rules, Notifications, non-admin navigation), `requests.spec.ts`
(Requests page), `setup.spec.ts` (wizard through Finish) and `status.spec.ts`
(Settings operational status).

```sh
make web-install
cd web && npx playwright install chromium && cd ..
make web-check
make web-test
```

On a minimal Linux host, Playwright may also require its documented browser OS
dependencies. The suite starts its own loopback-only Vite server on port 4173;
keep that port free. It never reuses an unrelated running server.

Tests cover bookmarked controls/reloads, URL paging and Back/Forward, cursor
expiry recovery, role-based actions, CSRF, last-subscriber confirmation, pending
action guards, error retention, stale response cancellation, keyboard controls,
and phone-width artwork/overflow. Test source/config are included in web-check.
Screenshots/traces are saved under `web/test-results/` on failure and ignored by
Git; a phone-layout screenshot is also produced on a successful layout test.
For a focused rerun: `cd web && npm test -- --grep 'phone layout'`.
# Mutating-endpoint role matrix

`internal/httpapi/role_matrix_test.go` runs the complete HTTP router against
unauthenticated, viewer, requester, and admin callers for every mutating route.
It uses valid bodies, sessions, and CSRF so permission failures are not masked
by malformed input or an unavailable backend. Denied requests must not invoke
the intended mutation; permitted requests must reach exactly that mutation with
the expected actor. Tests use local fakes, not a live OIDC provider or database.

| Mutation | Viewer | Requester | Admin |
| --- | --- | --- | --- |
| Create request | Forbidden | Allowed | Allowed |
| Resolve metadata | Allowed | Allowed | Allowed |
| Subscribe / leave | Allowed | Allowed | Allowed |
| Mark own notification read | Allowed | Allowed | Allowed |
| Approve / deny / cancel | Forbidden | Forbidden | Allowed |
| Source sync, user-role change, match accept/dismiss | Forbidden | Forbidden | Allowed |
| Logout | Allowed | Allowed | Allowed |
| Initial-admin claim | Matching identity + one-time token | Matching identity + one-time token | Matching identity + one-time token |

Every mutation rejects an unauthenticated caller. All authenticated roles are
also checked with missing/invalid CSRF and missing/untrusted Origin, including
endpoints the caller could not use even with valid CSRF. Bootstrap negative
cases cover a mismatched issuer/subject and invalid token; real single-use and
database concurrency guarantees remain covered by bootstrap integration tests.

`internal/httpapi/role_coverage_test.go` scans production Go registrations under
`internal/` and compares mutating method/path patterns to the same matrix table.
It expands literal method/action loops, flags unresolved dynamic expressions,
and rejects new methodless leaf registrations. Only the existing API-prefix
and SPA delegates are exempt from explicit methods. GET/HEAD/OPTIONS are not
part of this mutation inventory; OIDC's GET login/callback protocol retains its
separate authentication tests. New route-registration forms or a different
router must extend this guard rather than bypass it.

When adding a mutation, add a valid executable case to `mutationCases()` and
implement its deterministic fake. The four-role and CSRF loops automatically
exercise the new row. Additional action variants may share one route pattern,
as approve/deny/cancel do. The guard fails for both uncovered production routes
and stale matrix rows. Fixtures prove discovery of a new route, loop expansion,
and failure on unsupported or methodless registrations.

```sh
go test ./internal/httpapi -run 'Test(MutatingEndpoint|EveryMutating|RouteCoverageGuard|BootstrapClaimRequires)' -count=1
go test -race ./internal/httpapi
```

These tests run with `make test`; there is no new dependency or production
authorization-policy change.
# Setup draft tests

The first setup steps have tests at the agreed service, outbound-client,
PostgreSQL, full-router, and browser seams:

- `go test -race ./internal/setup/... ./internal/httpapi` checks resumability,
  safe validation failures, closed secret-reference fields, admin permissions,
  CSRF/Origin, revision conflicts, and bounded HTTP/network policy using fixtures.
- PostgreSQL integration tests cover atomic initialization, cross-store resume,
  stale/concurrent saves, validation revision binding, audit rollback, typed
  non-secret storage, migration round trips, and bootstrap-state preservation.
- `make web-test` now also runs the wizard's mocked browser tests, including
  resume, save-before-check, connection failure, conflict recovery, role gating,
  secret-reference-only forms, stale requests, and phone/keyboard use.

For repeated full integration runs, use a fresh dedicated disposable database
through `REQUESTARR_TEST_DATABASE_URL`. The existing reused test database can
accumulate open music fixtures and starve a globally limited alias lookup; do
not delete application data to work around that pre-existing test-isolation gap.
New setup tests never contact live OIDC/TMDB/MusicBrainz/IGDB services. Live
interactive sign-in proof and final configuration activation are outside this
first-step slice.
