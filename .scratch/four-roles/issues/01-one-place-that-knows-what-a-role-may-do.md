# 01: One place that knows what a role may do

**What to build:** Every question of the form "may this person do that?" is answered by one role module, and the server tells the web app what the signed-in person can do, so the interface never compares role names. Nothing changes for anyone: the three roles behave exactly as today, and `superuser` is not stored yet. This is the prefactor that the rest of the work stands on. (Spec: Implementation Decisions, role model and session answer; stories 54, 56.)

**Blocked by:** None (can start immediately)

**Status:** done

- [x] A role module defines the ranking of the four roles and what each may do (request, manage requests, manage the system); the three stored roles map onto it unchanged
- [x] The HTTP access check, the request, issue and notification services and the store's role checks ask the module instead of comparing role strings; a search of production code finds no remaining comparison against a role name outside the module
- [x] The session endpoint returns the role and its capabilities (request, manage requests, manage system), documented in the OpenAPI contract in the same change
- [x] The web app decides navigation, Settings, Issues, Requests, the request modals and the Get started page from the capabilities and no longer compares role names
- [x] An automated check fails when production Go code or the web app compares a role name outside its allowed places, in the style of the existing slug and secret-name checks
- [x] The existing role matrix and Playwright suites pass without edits to their expectations; `scripts/check.sh` passes


## Comments

All criteria checked by running real code, not mocks:

- Role module: `internal/domain/role.go` (four ranked roles, `Capabilities`, `CanRequest`, `CanManageRequests`, `CanManageSystem`, `Stored`, SQL role lists); `role_test.go` covers the table. `superuser` is defined but `Stored()` is false, so nothing writes it yet.
- No role-name comparison remains: `TestRoleNamesAreSpelledOnlyInTheRoleModule` parses all production Go (string literals and SQL `role = '...'`) and passes; allowed files are `domain/role.go`, `domain/request.go` (subscription source) and `auth/local.go` (the username `admin`). The HTTP access check, request, issue and notification services and the store (`ChangeUserRole`, last-admin count, first-sign-in and issue notices, local administrator status) all go through it. PostgreSQL integration tests pass on a fresh database.
- Session: `GET /auth/me` returns `capabilities` (`TestAuthMeReturnsTheRoleAndItsCapabilities`, and the five-field contract test in `admin_recovery_test.go`); documented in `api/openapi.yaml`.
- Web: `TestWebAppNeverComparesRoleNames` scans `web/src` and passes; App, modals, Issues, Requests and DevelopmentLogs read `session.capabilities`.
- Playwright (317 tests) and `scripts/check.sh` pass. The role matrix expectations were not edited. Only fixtures changed: the `/auth/me` mocks now include `capabilities` via `web/tests/capabilities.ts`, and the contract test counts five fields.
- For tickets 02/03/06: add the superuser level by changing `CanManageSystem` (and a new `CanManageAuthentication` or similar) in `role.go`; `RolesThatManageSystem()` feeds the last-superuser SQL, and `Stored()` must start admitting `superuser` with the migration. The access label `accessAdmin` prints as "manage".
