# 05: Per-user search limit

Parent: `.scratch/name-search/spec.md`

**What to build:** One person's burst of searches cannot use up the provider budgets the whole household shares. The search endpoint enforces a modest per-user limit with built-in defaults and no new admin setting. When it trips, the API answers with a retry hint and the UI shows a clear "slow down, try again in a moment" message that does not clear existing results and does not look like "no results". The server's HTTP layer has no general limiter today (only the issue-report one), so this is new, small, in-process code.

**Blocked by:** 03

**Status:** done

- [x] Exceeding the limit returns 429 with a retry-after hint and a documented error code, in the OpenAPI contract
- [x] The limit is per user: one user at the limit does not block another
- [x] Cached or single-flight hits count sensibly (a repeat within the fresh window does not hit a provider; decide and test whether it counts toward the limit)
- [x] The UI shows a distinct message, keeps the last results, and recovers after the wait without a reload
- [x] The limit does not affect `POST /resolve` or previews
- [x] `make fmt test vet web-check` and the browser tests pass

## Outcome

- Per-user token bucket (burst 30, one more every 250 ms; originally burst 10 and one per second, loosened after review because a typeahead typist at 300 to 400 ms per keystroke would have been limited), in-process, injectable clock (`internal/httpapi/searchlimit.go`). 429 `search_rate_limited` with `Retry-After`, in `api/openapi.yaml`.
- Decision: every well-formed search spends a token, including fresh-cache hits and joined single-flight queries. The limit caps a client's request rate on the server; it is not a provider-call count. Invalid (400) requests cost nothing. `/search/types` and `POST /resolve` are not limited.
- UI: keeps the last results, shows an alert, retries once by itself after the hinted wait.
- Verified: Go tests in `searchlimit_test.go` (clock-driven), Playwright test in `web/tests/search.spec.ts`. Browser suite run on port 4273 because 4173 was held by another tree; the four specs hard-coding 4173 failed only for that reason, plus the known home-recent failure.
