# 05: Per-user search limit

Parent: `.scratch/name-search/spec.md`

**What to build:** One person's burst of searches cannot use up the provider budgets the whole household shares. The search endpoint enforces a modest per-user limit with built-in defaults and no new admin setting. When it trips, the API answers with a retry hint and the UI shows a clear "slow down, try again in a moment" message that does not clear existing results and does not look like "no results". The server's HTTP layer has no general limiter today (only the issue-report one), so this is new, small, in-process code.

**Blocked by:** 03

**Status:** ready-for-agent

- [ ] Exceeding the limit returns 429 with a retry-after hint and a documented error code, in the OpenAPI contract
- [ ] The limit is per user: one user at the limit does not block another
- [ ] Cached or single-flight hits count sensibly (a repeat within the fresh window does not hit a provider; decide and test whether it counts toward the limit)
- [ ] The UI shows a distinct message, keeps the last results, and recovers after the wait without a reload
- [ ] The limit does not affect `POST /resolve` or previews
- [ ] `make fmt test vet web-check` and the browser tests pass
