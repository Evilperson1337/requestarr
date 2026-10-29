# 06: One route policy table and Deps struct for httpapi

**What to build:** Replace New, NewWithMetadata, NewWithSync and NewWithSetup with one Deps struct of required dependencies that fails at startup; update every call site including tests. Add a declarative table of (method, path, access: public/session/admin, CSRF) registered through one helper; delete the ~10 hand-copied admin gates and the string literals in protectedMutation. Move the POST /requests error-to-status mapping next to the requests handlers. Replace the field tables in httpapi/setup.go with strict decoding (DisallowUnknownFields) into the draft types. role_matrix and role_coverage tests iterate the table.

**Blocked by:** 02 (strict decoding relies on the registry-driven draft types)

**Status:** done

- [ ] Missing capability fails at startup, not as an absent route or 503
- [ ] One test enumerates the table for role and CSRF policy
- [ ] No type assertions on readiness to discover capabilities
- [ ] openapi.yaml still matches the routes

## Progress

Done: `internal/httpapi/access.go` adds the route table. Every route is registered through `rt.handle(pattern, access, fn)` (public, session or admin), which answers 401 and 403 itself, so the ~10 hand-copied admin gates in `router.go`, `setup.go`, `secrets.go`, `suggestions.go` and `development_logs.go` are gone. CSRF is derived from the same table (every state-changing route except the auth routes that check it themselves), and `protectedMutation` with its string literals is deleted; the request pipeline asks the table, so the policy cannot drift from the routes. `TestRouteTablePolicy` checks the table, and `TestEveryMutatingEndpointHasARoleMatrixRow` now reads it instead of parsing source, so the AST coverage guard and its self-tests are deleted. The POST /requests error mapping is `writeCreateRequestError`. `hasRequiredDraftFields` reads the required keys from the `setup.Settings` type by reflection instead of per-section lists. The existing role/CSRF matrix (every mutating route, four roles, four CSRF failures) passes unchanged, which is the check that the table matches the old behavior. Go and integration tests pass.

Not done (open acceptance items):
- The `Deps` struct. `New`, `NewWithMetadata`, `NewWithSync` and `NewWithSetup` remain, and the router still finds requests, inbox, job runs, operational status, user management, match review, development logs and setup state by type assertion on `readiness`. A missing capability is still a 503 or an absent route, not a startup error.
- Plan for it: a `Deps` struct of typed fields validated by `New(Deps) (http.Handler, error)`, `app.Run` filling it from the store, and a test-only helper that builds `Deps` from the existing fake backends so the ~60 test call sites change to one helper call. `setupWithSecrets` goes with it.
- Strict decoding with `DisallowUnknownFields` does not replace the required-field check (it rejects extras, not missing keys), so the reflection check is what replaces the tables.

## Follow-up (done)

`httpapi.Deps` and `NewRouter(Deps)` replace `New`, `NewWithMetadata`, `NewWithSync` and `NewWithSetup`. Every dependency except `DevelopmentLogs` is required, and `NewRouter` fails at startup naming what is missing. The router has no type assertions on `readiness`; `app.Run` fills `Deps` from the store, `opstatus.Service`, the metadata service, the syncer, the setup service and the secrets service. `statusStore` and `setupWithSecrets` are deleted. In tests, `testdeps_test.go` keeps the old constructor names and builds `Deps` from one fake backend, with absent stubs for the rest. Five tests that asserted a 503 for a missing capability were removed, since that state can no longer exist; `TestNewRouterNamesEveryMissingDependency` replaces them.
