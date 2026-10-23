# 19: Role matrix tests

**What to build:** Every mutating endpoint is tested against unauthenticated, viewer, requester and admin.

**Blocked by:** 05 Viewer demotion and last-admin guard, 08 Cancel and leave a Request

**Status:** done

- [x] A table-driven test enumerates every mutating endpoint and role
- [x] Unauthenticated requests are rejected; viewer cannot create; only admins reach admin endpoints
- [x] Adding a new mutating endpoint without a row fails the test or is flagged

**Delivered:** Full-router role matrix for 12 mutating route patterns through
14 action scenarios, including approve/deny/cancel variants, across anonymous,
viewer, requester, and admin callers. Valid payloads/sessions prove successful
paths, while denied paths cannot invoke the intended fake mutation. All
authenticated roles also receive missing/wrong CSRF and missing/untrusted
Origin coverage. Bootstrap identity/token authorization is preserved and has
explicit negative cases; no production authorization policy changed.

The source-registration guard compares all production internal Go registrations
with the executable matrix, expands literal method/action loops, and flags
unsupported dynamic or methodless leaves. Fixtures cover new routes, safe
methods, capture idioms, reassignment, shadowing, and nested-loop scope handling.
Tests and documentation only; no dependency, API, or schema changes.

**Validation:** `make fmt`, `make test`, `make vet`, `make web-check`,
`make web-test` (12 browser tests), `make build`, `go test -race ./internal/httpapi`,
and `git diff --check` passed. The full PostgreSQL integration suite passed
through `make test-integration REQUESTARR_POSTGRES_PORT=5433` with
`REQUESTARR_TEST_DATABASE_URL` targeting a fresh temporary test database.
Independent Standards and Spec reviews completed; one Standards guard-scope
finding was fixed, regression-tested, and confirmed resolved in follow-up.

**Validation caveat:** The reused `requestarr_test` database failed the existing
Plex music alias fixture because 54 pending music requests occupied its globally
limited 50-row lookup. That failure also reproduced in isolation. The existing
database was inspected read-only and preserved; only the newly created
validation database was cleaned up. Existing integration-fixture cleanup/isolation
is outside this ticket. The HTTP matrix itself needs neither PostgreSQL nor live
OIDC/provider services.
