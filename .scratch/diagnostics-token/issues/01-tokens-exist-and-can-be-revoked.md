# 01: Diagnostics tokens exist, expire and can be revoked

Status: ready-for-agent

**What to build:** The storage and service for diagnostics tokens: create (shown once), list, revoke, expiry, the five-token limit, and the audit rows. No HTTP route uses a token yet. (Spec stories 1-6, 10; Implementation Decisions: token, managing tokens, audit.)

**Blocked by:** none

- [ ] Migration 42 adds `diagnostics_tokens` with a unique token hash; the down migration drops it
- [ ] A service creates a token (`rqd_` plus 32 random bytes), returns the value once, stores only its keyed hash; list never returns a value or hash
- [ ] Expiry is required, defaults to 30 days and is refused above 90; the sixth active token is refused
- [ ] Revoke is immediate; create and revoke write value-free audit rows with the actor
- [ ] A lookup returns the token only while it is unexpired, unrevoked and its creator is an enabled superuser who can sign in
- [ ] PostgreSQL test: the plain value is nowhere in the database; a canary value never reaches the log or the audit
- [ ] `scripts/check.sh` passes

## Comments
