# 02: A bearer token reaches only the routes marked for it

Status: done

**What to build:** The route table accepts `Authorization: Bearer rqd_...` on GET routes explicitly marked token-readable, and refuses it everywhere else. Mark `GET /admin/logs`, `/admin/status`, `/admin/job-runs`, `/admin/library-connections` and `/admin/secrets`. (Spec stories 5-10; Implementation Decisions: where it works, who may use it, abuse.)

**Blocked by:** 01

- [x] The route table has a per-route token-readable marker; a route without it answers 401 to a token, and so does every non-GET
- [x] A token in a query parameter or cookie is ignored; a request with the header is judged as a token request only
- [x] `TestTokenReadableRoutesAreExactlyThese` pins the list
- [x] Expired, revoked, unknown, malformed and wrong-prefix tokens answer the same 401; demoting or disabling the creator ends the token on the next request
- [x] Failed attempts are throttled per client address and logged at warning level with a fixed reason; a token is limited to 60 requests a minute; `last_used_at` is written at most once a minute
- [x] A canary token never appears in any log line or response
- [x] `scripts/check.sh` passes

## Comments

How each criterion was checked (all in `internal/httpapi/diagnostics_bearer_test.go` unless noted; router tests use an in-memory token store and a moveable clock, `scripts/check.sh` including PostgreSQL integration and Playwright passed):
- Marker: `routePolicy.TokenReadable`, set only by `routeTable.markTokenReadable` (panics at startup for an unregistered or non-GET route). `TestATokenIsRefusedOnEveryRouteNotMarkedAndEveryNonGet` sends a valid token to every other registered route (every method) and POST/PUT/PATCH/DELETE/HEAD/OPTIONS to the five, plus an unknown path: all 401. `GET /version` used to bypass the pipeline through a duplicate registration on the outer mux; I removed that duplicate so it too answers 401 to a token.
- Query/cookie: `TestATokenInTheQueryOrACookieIsIgnoredAndTheHeaderIsJudgedAlone`: token in query or cookie gives 401; a bad header plus a valid session cookie gives 401; a valid header works with no cookie.
- Pin: `TestTokenReadableRoutesAreExactlyThese`; `TestOpenAPIMarksTheTokenReadableOperations` (openapi_token_test.go) ties `x-token-readable: true` to the table.
- Same 401: `TestEveryRefusedTokenAnswersTheSame401AndLogsAFixedReason` covers unknown, wrong prefix, malformed (empty, wrong scheme, stray spaces), revoked, expired and creator-not-superuser; bodies are identical and each logs one warning with a fixed `reason`. Caveat: the "demoted or disabled creator" case flips `CreatorCanSignIn` on the in-memory store. The real store's rule (role and disabled local credential) is what ticket 01's PostgreSQL test `TestLookupFollowsTheCreatorsRoleAndAbilityToSignInIntegration` checks, and the gate calls `Lookup` on every request, so nothing is cached between requests.
- Throttle and rate: `TestRepeatedFailuresFromOneAddressAreRefusedWithoutCheckingTheToken` (six failures hold the address; the store sees no lookup for the next request, 429 with Retry-After, another address unaffected, delay ends), `TestATokenIsLimitedTo60RequestsAMinute` (61st is 429, next minute works), `TestLastUsedIsWrittenAtMostOnceAMinute` (one store write for five requests).
- Canary: `TestACanaryTokenNeverAppearsInALogLineOrResponse` drives good, altered and expired use across routes and checks the token body is in no log output and no response body or header; `internal/guardrails` still passes (the log fields are `reason`, `client_addr`, `method`, `path`).
- `scripts/check.sh` exit 0.

Notes for tickets 03 and 04:
- Gate: `auth.Handler.AuthenticateBearer` / `RefuseBearer` in `internal/auth/diagnostics_bearer.go`; wired in `apiHandler` in `internal/httpapi/router.go`. `auth.Handler.Diagnostics` (`*auth.DiagnosticsTokens`) is set in `internal/app/app.go`; nil refuses every bearer request.
- Any request with an Authorization header is judged as a token request only. Not-marked or non-GET gives 401 without a throttle count; bad tokens give 401 and count against the address; a held address gets 429 before the token is looked at.
- A token acts as `auth.DiagnosticsPrincipal(tok)`: ID `diagnostics-token:<id>`, role `domain.DiagnosticsTokenRole` (superuser), carried in the request context (`tokenUserKey`). The route wrapper skips the session when it is present. Ticket 03's management routes must stay unmarked so a token can never create or revoke tokens.
- Throttle: per-address delay reuses the sign-in address table (`Throttle.FailAddress`, `Throttle.AddressHeld`, grace 5 then 1s doubling to 15s). Rate: `auth.DiagnosticsRateLimit` (60) per `auth.DiagnosticsRateWindow` (1 minute), fixed window per token, in memory.
- OpenAPI: `x-token-readable: true` on the five GETs and the `diagnosticsBearer` scheme under `components.securitySchemes` (no per-operation `security` yet).
