# 01: Diagnostics tokens exist, expire and can be revoked

Status: done

**What to build:** The storage and service for diagnostics tokens: create (shown once), list, revoke, expiry, the five-token limit, and the audit rows. No HTTP route uses a token yet. (Spec stories 1-6, 10; Implementation Decisions: token, managing tokens, audit.)

**Blocked by:** none

- [x] Migration 42 adds `diagnostics_tokens` with a unique token hash; the down migration drops it
- [x] A service creates a token (`rqd_` plus 32 random bytes), returns the value once, stores only its keyed hash; list never returns a value or hash
- [x] Expiry is required, defaults to 30 days and is refused above 90; the sixth active token is refused
- [x] Revoke is immediate; create and revoke write value-free audit rows with the actor
- [x] A lookup returns the token only while it is unexpired, unrevoked and its creator is an enabled superuser who can sign in
- [x] PostgreSQL test: the plain value is nowhere in the database; a canary value never reaches the log or the audit
- [x] `scripts/check.sh` passes

## Comments

How each criterion was checked (against real PostgreSQL via `make test-integration`, which `scripts/check.sh` runs):
- Migration 42: `TestDiagnosticsTokenMigrationRollsBackAndForwardIntegration` checks the unique token_hash index, runs the down migration (table gone) and the up migration again. The down also restores the narrower audit event check.
- Service: `internal/auth/diagnostics_token_test.go` (unit, in-memory store) covers the `rqd_` + 32 base64url bytes format, stored hash == `auth.Hash(secret, value)`, and that a list row has no value or hash field. The plain-token test below reads the real database.
- Expiry and limit: default 30 days, 90 allowed, above 90 or negative refused (unit). The limit is enforced under an advisory lock; `TestSixthActiveTokenIsRefusedEvenWhenCreatedTogetherIntegration` races 8 creates and gets exactly 5; a revoke or an expiry frees a slot.
- Revoke and audit: `TestLookupFollowsTheCreatorsRoleAndAbilityToSignInIntegration` shows lookup refused right after revoke; `TestCreateAndRevokeAuditTheActorAndNeverTheValueIntegration` shows one created and one revoked row, each with actor, address and token id, and no row for a refused revoke. Audit rows have no column that could hold a value.
- Lookup rule: the same lookup test changes the creator's role (admin/requester/viewer), disables their local credential, and moves the expiry, checking the refusal reason each time.
- Plain value absent: `TestThePlainTokenIsNowhereInTheDatabaseOrLogIntegration` scans every table as text for the token body and captures slog output across create, lookup, touch, list, revoke and a refused lookup.
- `scripts/check.sh` passed at the final commit (see the report).
Notes: the service logs nothing itself; the HTTP layer (ticket 02) logs the fixed `DiagnosticsRefusal.Reason`. "Can sign in" means a role allowed by `RolesThatManageAccess` (superuser) and no disabled local credential, the same rule as the last-superuser guard.
