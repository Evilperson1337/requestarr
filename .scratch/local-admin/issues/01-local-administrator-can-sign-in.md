# 01: The local administrator can sign in

**What to build:** On a fresh install the built-in local administrator exists at first startup and its one-time password is in the log. The operator signs in at a local form, is forced to choose their own password, and lands in the app as an administrator, with no identity provider involved. This is the core vertical slice: storage, hashing, creation, sign-in with protection and audit, password change, and the minimal screens. (Spec stories 1-5, 9-15, 21; D040.)

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] A new migration adds the local credential (user, password hash with scheme, parameters and salt, must-change flag, disabled flag, timestamps) and an authentication audit table; the built-in account is an ordinary admin user row marked as built-in; migrations are append-only with up and down files
- [x] A small hashing module hashes and verifies passwords with the standard library's PBKDF2-SHA256 at the current OWASP iteration count, a per-password salt, constant-time comparison, scheme and parameters stored with the hash, and a needs-rehash check; no new dependency
- [x] At startup with no users, the account (`admin`) is created with a random password, or `REQUESTARR_ADMIN_PASSWORD` when set, must-change set; the generated password appears once, in one clear log line that says what to do next, and nowhere else; with users already present nothing is created
- [x] A local sign-in endpoint accepts a username and password and creates a session by the existing mechanism (opaque token hashed at rest, cookie, CSRF); while must-change is set the session can do nothing except change the password
- [x] Protection: failed attempts add an exponentially growing, capped delay per account and per client address (in memory, documented); there is no lockout; every success is written to the audit table and failures and throttled attempts are written coalesced (at most one row per account or address per minute, with a global cap and 90-day pruning, D040); responses and timing never reveal whether the username or the password was wrong
- [x] The password-change endpoint verifies the current password, enforces 12 to 128 characters with no composition rules, ends the account's other sessions, and clears must-change
- [x] The local form has its own address that is always served; a minimal forced-change screen follows the first sign-in; the existing OIDC sign-in is unchanged by this ticket
- [x] Tests: handler tests with a fake store for every case in the spec's seam 1 (wrong password, unknown username, delay growth and cap, no lockout, audit events, forced change, session revocation, length limits, uniform responses) and for unauthenticated, viewer, requester and admin paths; PostgreSQL integration tests for creation on a fresh database and none when users exist; a test that the generated password appears in the log line and in no other output
- [x] The OpenAPI contract describes the new routes and the contract test passes
- [x] `scripts/check.sh` passes

## Answer

Ticket01's local-administrator vertical slice is verified, including deterministic concurrency regressions and PostgreSQL failure-path tests. All acceptance checks above passed. Existing OIDC behavior is preserved; tickets02+ were not implemented.

## Comments

### 2026-11-05 — Resumed verification and acceptance evidence

- Started from implementation `0e21d3e93f51917a561da2f0d4c2dd1d7e6aa7bd` on `feat/local-admin`, only in the integration worktree. The root remained clean on `feat/unraid-deployment` at `a5dbdb4af7bc43e6e5541d79a1a715e3701695fd`; unrelated worktrees were preserved.
- Initial required gates passed, but Standards/Spec reviews found stale-credential session issuance, overlapping password verification, non-atomic password-change auditing, and credential-bearing test diagnostics. These were fixed test-first in the separate `fix/local-admin-ticket01-verification` effort worktree, not directly in integration.
- Effort commit `fc14075a1ba1903d69b8ed92133122f20b77ac38` was cherry-picked as `614a4fb73ee3e05e315ddc9efe94c32ba4aeed67`. Follow-up `b1d6a820f3efc1adbdd63c0d65c5597238742f18` was cherry-picked as `ca6010cbfb65b723951c7ff3f2b386bb5320f6b4`, so a rejected stale sign-in is audited as failure, not success. New barrier waits are bounded; no sleeps are used by the concurrency regressions.
- On code revision `ca6010cbfb65b723951c7ff3f2b386bb5320f6b4`: `make fmt`, `make test`, `make vet`, `make web-check`, and the complete `scripts/check.sh` all exited 0. The script actually ran deployment/export checks, PostgreSQL integration, frontend typecheck, and all 250 Playwright tests (58.5 seconds); no gate was skipped. `make build` also exited 0.
- Additional uncached Go run: 539 top-level tests / 1,662 test-and-subtest pass events, zero test skips/failures; 34 tested packages pass and five packages have no test files. PostgreSQL JSON run: 124 top-level tests / 161 test-and-subtest pass events, zero skips/failures. Race-enabled authentication/router tests exited 0.
- PostgreSQL ran on a new effort-owned PostgreSQL 17 container with a dynamically allocated loopback port, not an existing worktree's database. Full integration runs started from an empty dedicated database. Playwright used port 4197 to avoid the existing server. Logs and the connection environment are private; credentials were redirected, never printed or committed.
- Final focused Standards and Spec review: zero remaining blocking findings on either axis. No live OIDC-provider or deployed reverse-proxy/browser exercise is claimed: browser tests use mocked API fixtures, and the real PostgreSQL handler flow covers bootstrap, sign-in, forced change, audit, and non-disclosure. Optional OIDC startup/runtime configuration and other later-ticket requirements remain outside this ticket.
- Context, architecture, acceptance-to-test pointers, and exact log locations: [ticket01 verification record](../ticket01-verification.md). Private integration log ledger: [gates.tsv](../../../tmp/ticket01-verification-20261105/gates.tsv); full verified script: [check-verified.log](../../../tmp/ticket01-verification-20261105/check-verified.log). Private effort red/green index: [EVIDENCE.txt](../../../../local-admin-ticket01-verification/tmp/ticket01-fixes/EVIDENCE.txt). Raw logs are ignored local artifacts and must not be published without redaction.
