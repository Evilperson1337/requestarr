# 03: Create and revoke tokens in Settings, and the API contract

Status: done

**What to build:** `GET`, `POST`, `DELETE /admin/diagnostics-tokens`, superuser-only, with a card in Settings, Logs to create (name and lifetime; the value shown once with a copy button), list (name, created, expires, last used) and revoke. The OpenAPI contract gains the bearer scheme and the token-readable marker. (Spec stories 1-4; Implementation Decisions: managing tokens, contract.)

**Blocked by:** 01, 02

- [x] The three routes are superuser-only, CSRF on change, `no-store`; a token cannot call them
- [x] The creation response is the only place the value appears; the page says so and clears it when dismissed
- [x] `TestSuperuserOnlyRoutesAreExactlyThese` and the OpenAPI contract tests are updated; a test ties `x-token-readable` to the route table
- [x] Playwright with axe at desktop and phone widths covers create, list and revoke (mocked API in the browser tests; checked live on 2026-11-08, see Comments)
- [x] `scripts/check.sh` passes

## Comments

How each criterion was checked (Go in `internal/httpapi/diagnostics_tokens_test.go` unless noted; `scripts/check.sh` exit 0, including PostgreSQL integration and the full Playwright run):
- Routes: `TestTokenManagementRoutesArePolicedSuperuserOnlyWithCSRF` reads the route table (superuser, CSRF on POST and DELETE, not token-readable). `TestOnlyASuperuserManagesTokens`: admin, requester, viewer get 403 and anonymous 401. `TestTokenManagementChangesNeedCSRFAndSameOrigin`: missing or wrong CSRF and a foreign or missing Origin are 403 and change nothing. `Cache-Control: no-store` is asserted on create, list and revoke. `TestATokenCannotManageTokens`: a valid bearer token gets 401 on all three and the token table is unchanged. The four-role matrix has rows for create and revoke (`role_matrix_test.go`), so the CSRF and role matrices cover them too.
- Value once: `TestASuperuserCreatesListsAndRevokes...` shows the value only in the 201 body (the list body contains neither the new value nor the first token), that the value works as a bearer token, and that revoking ends it at once. The storage side (no plain value in any table or log) is ticket 01's PostgreSQL test. The page text and dismissal are `web/tests/diagnostics-tokens.spec.ts`: the page says "This is the only time the token is shown", the group takes focus, Copy puts the value on the clipboard, "I have saved the token" removes it from the DOM (`page.content()` has no trace), a reload and a revoke of the same token also clear it. The browser is real but the API is a Playwright mock, so the UI half has not been run against the real server.
- Pinned lists: `TestSuperuserOnlyRoutesAreExactlyThese` gains the three routes; `TestOpenAPINamesTheSuperuserOnlyOperations` and `TestOpenAPIMatchesTheRouteTable` pass with the new paths (`x-required-role: superuser`); the `diagnosticsBearer` scheme was already there. `x-token-readable` stays tied to the route table by ticket 02's `TestOpenAPIMarksTheTokenReadableOperations` and `TestTokenReadableRoutesAreExactlyThese`, which still pass with the management routes unmarked.
- Playwright: desktop (1280) and phone (390) run create, list, copy, dismiss, reload, revoke, with axe (no violations) and no horizontal overflow before and after each step; plus revoking the on-screen token and an admin seeing no card and making no request.
- `scripts/check.sh`: exit 0.

Notes:
- Request body `{"name","lifetimeDays"}` (0 or omitted = 30, at most 90); response `{"value","token"}`; list `{"tokens","maxActive"}`. Errors: 422 `invalid_diagnostics_token`, 409 `diagnostics_token_limit`, 404 `diagnostics_token_not_found` (also for a malformed id), 503 `diagnostics_tokens_unavailable` if no service is wired.
- Added `auth.Handler.ClientAddr` so the audit row records the same address as the sign-in audit. The in-memory token store in `diagnostics_bearer_test.go` now lists and refuses revoking an unknown token.
- axe found two things in the first draft that are fixed: a duplicate landmark name (region named like its section) and an h4 under an h2.

Review follow-up: set to `needs-review`. The Playwright criterion is unticked because the browser run uses a mocked API (`page.route`), so the card has never been run against a real server. The live check that is missing: start Requestarr with PostgreSQL (`make stack-up`), sign in as a superuser, and in Settings, Logs create a token, see the value once, use it with `scripts/agent/requestarr-diag.sh status`, revoke it, and confirm the next call is refused 401; then repeat at a phone width. The Go side (routes, CSRF, no-store, audit) is checked against the real router and, for storage, PostgreSQL.

- Live check on the real server (2026-11-08, rc.8 image with the token code, run against https://requests.example.com by `scripts/agent/requestarr-diag.sh`): the operator created a token in the Settings, Logs card and listed it; the token read status, runs, connections, secrets and logs (the five readable routes) and was answered 401 on users, authentication settings, requests, the token list, token create, sync now and writing a secret; a made-up `rqd_` token was 401 and logged as `reason=unknown` without the value; a Basic `Authorization` header on a public route still answered 200. The operator then revoked the token in the card, and the next request with it answered 401 at once. That completes the live check this ticket was waiting on.
