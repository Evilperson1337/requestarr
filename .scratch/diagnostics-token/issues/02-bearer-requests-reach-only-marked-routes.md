# 02: A bearer token reaches only the routes marked for it

Status: ready-for-agent

**What to build:** The route table accepts `Authorization: Bearer rqd_...` on GET routes explicitly marked token-readable, and refuses it everywhere else. Mark `GET /admin/logs`, `/admin/status`, `/admin/job-runs`, `/admin/library-connections` and `/admin/secrets`. (Spec stories 5-10; Implementation Decisions: where it works, who may use it, abuse.)

**Blocked by:** 01

- [ ] The route table has a per-route token-readable marker; a route without it answers 401 to a token, and so does every non-GET
- [ ] A token in a query parameter or cookie is ignored; a request with the header is judged as a token request only
- [ ] `TestTokenReadableRoutesAreExactlyThese` pins the list
- [ ] Expired, revoked, unknown, malformed and wrong-prefix tokens answer the same 401; demoting or disabling the creator ends the token on the next request
- [ ] Failed attempts are throttled per client address and logged at warning level with a fixed reason; a token is limited to 60 requests a minute; `last_used_at` is written at most once a minute
- [ ] A canary token never appears in any log line or response
- [ ] `scripts/check.sh` passes

## Comments
