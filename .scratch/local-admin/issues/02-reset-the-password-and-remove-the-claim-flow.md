# 02: Reset the password from the server, and remove the claim flow

**What to build:** A forgotten local administrator password is fixed with `requestarr reset-admin-password`, and the first-admin claim flow it replaces is gone. (Spec stories 16-17, plus the removal in the spec; D040.)

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] `reset-admin-password` generates a random password, stores its hash with must-change set, ends the account's sessions, prints the password once and nothing else sensitive, and fails with a clear message when the account does not exist; it never prints a hash
- [ ] The `setup-token` subcommand, the claim page, `POST /setup/claim-admin`, the `firstAdminClaim` field on the current-user response, the claim-related setup status, the startup claim hint and their tests, OpenAPI entries and documentation are removed; the database tables stay
- [ ] A person becomes an admin by being promoted in Settings, Users by an existing admin; nothing else grants the admin role
- [ ] Tests through the binary's entry point against a test database (prints once, forces a change at next sign-in, ends sessions, fails with no account, no hash in any output) and the existing role-matrix and contract tests updated; nothing references a removed route
- [ ] The docs that describe first run (`docs/FIRST_RUN.md`, `docs/UNRAID.md` section on the first admin, `docs/OIDC_AUTH.md`, `docs/MANUAL_TESTING.md`) describe signing in as the local administrator and the reset command
- [ ] `scripts/check.sh` passes
