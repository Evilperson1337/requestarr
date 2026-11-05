# 01: The local administrator can sign in

**What to build:** On a fresh install the built-in local administrator exists at first startup and its one-time password is in the log. The operator signs in at a local form, is forced to choose their own password, and lands in the app as an administrator, with no identity provider involved. This is the core vertical slice: storage, hashing, creation, sign-in with protection and audit, password change, and the minimal screens. (Spec stories 1-5, 9-15, 21; D040.)

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] A new migration adds the local credential (user, password hash with scheme, parameters and salt, must-change flag, disabled flag, timestamps) and an authentication audit table; the built-in account is an ordinary admin user row marked as built-in; migrations are append-only with up and down files
- [ ] A small hashing module hashes and verifies passwords with the standard library's PBKDF2-SHA256 at the current OWASP iteration count, a per-password salt, constant-time comparison, scheme and parameters stored with the hash, and a needs-rehash check; no new dependency
- [ ] At startup with no users, the account (`admin`) is created with a random password, or `REQUESTARR_ADMIN_PASSWORD` when set, must-change set; the generated password appears once, in one clear log line that says what to do next, and nowhere else; with users already present nothing is created
- [ ] A local sign-in endpoint accepts a username and password and creates a session by the existing mechanism (opaque token hashed at rest, cookie, CSRF); while must-change is set the session can do nothing except change the password
- [ ] Protection: failed attempts add an exponentially growing, capped delay per account and per client address (in memory, documented); there is no lockout; every success and failure is written to the audit table; responses and timing never reveal whether the username or the password was wrong
- [ ] The password-change endpoint verifies the current password, enforces 12 to 128 characters with no composition rules, ends the account's other sessions, and clears must-change
- [ ] The local form has its own address that is always served; a minimal forced-change screen follows the first sign-in; the existing OIDC sign-in is unchanged by this ticket
- [ ] Tests: handler tests with a fake store for every case in the spec's seam 1 (wrong password, unknown username, delay growth and cap, no lockout, audit events, forced change, session revocation, length limits, uniform responses) and for unauthenticated, viewer, requester and admin paths; PostgreSQL integration tests for creation on a fresh database and none when users exist; a test that the generated password appears in the log line and in no other output
- [ ] The OpenAPI contract describes the new routes and the contract test passes
- [ ] `scripts/check.sh` passes
