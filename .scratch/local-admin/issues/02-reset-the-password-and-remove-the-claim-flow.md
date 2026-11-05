# 02: Reset the password from the server, and remove the claim flow

**What to build:** A forgotten local administrator password is fixed with `requestarr reset-admin-password`, and the first-admin claim flow it replaces is gone. (Spec stories 16-17, plus the removal in the spec; D040.)

**Blocked by:** 01

**Status:** resolved

- [x] `reset-admin-password` generates a random password, stores its hash with must-change set, ends the account's sessions, prints the password once and nothing else sensitive, and fails with a clear message when the account does not exist; it never prints a hash
- [x] The `setup-token` subcommand, the claim page, `POST /setup/claim-admin`, the `firstAdminClaim` field on the current-user response, the claim-related setup status, the startup claim hint and their tests, OpenAPI entries and documentation are removed; the database tables stay
- [x] A person becomes an admin by being promoted in Settings, Users by an existing admin; nothing else grants the admin role
- [x] Tests through the binary's entry point against a test database (prints once, forces a change at next sign-in, ends sessions, fails with no account, no hash in any output) and the existing role-matrix and contract tests updated; nothing references a removed route
- [x] The docs that describe first run (`docs/FIRST_RUN.md`, `docs/UNRAID.md` section on the first admin, `docs/OIDC_AUTH.md`, `docs/MANUAL_TESTING.md`) describe signing in as the local administrator and the reset command
- [x] `scripts/check.sh` passes

## Comments

- 2026-11-05: Claimed in isolated worker `feat/local-admin-ticket02`, based on integration `1bbd461`. Confirmed TDD seams: binary + disposable PostgreSQL; HTTP/contracts/roles; browser fixtures; deployment-script checks. Scope limited to ticket02; ticket03/runtime OIDC deferred.

- 2026-11-05: Acceptance passed on implementation `534cdd73883958d454ee5b149a4184aa8ae6b46e`; resolved only after all required gates and `scripts/check.sh` passed with real PostgreSQL and 248 Playwright tests. [Verification and merger handoff](../ticket02-verification.md) records all six red/green slices, role/contract and audit-failure coverage, exact commands, counts, private log paths, and limits. Uncached results: 524 unit tests and 121 PostgreSQL tests, zero failures/skips; race and production build pass. Latest `feat/local-admin` (`1bbd461`) was merged into the worker (already up to date), not vice versa. Root remains clean at `a5dbdb4`; ticket01 verification and unrelated worktrees preserved. Documentation-only evidence/resolution follows the verified implementation commit. No PR; separate merger owns integration.
