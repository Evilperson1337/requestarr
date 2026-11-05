# 03: OIDC is configured at runtime

**What to build:** The server starts with no OIDC configuration at all, and an admin can save, test, enable, disable and discard OIDC settings through an API, applying at once. The environment wins as a unit and its conflicts are computed in one place. (Spec stories 6, 24-25, 27-30, 32-38; D040.)

**Blocked by:** 01

**Status:** claimed

**Worker:** `feat/local-admin-ticket03`, `.claude/worktrees/agent-a884313af677de3a3`; base `1e5a13d38ed045159b3151877e1d3cdf59a9cd21`. Confirmed TDD seams: pure state, app/runtime service, HTTP/OpenAPI, PostgreSQL settings/secrets.

- [ ] `REQUESTARR_OIDC_*` are no longer required at startup; an instance with none starts and serves the local sign-in; an instance with all three set behaves as before
- [ ] Non-secret settings (enabled, provider name, issuer, client ID) are saved in the settings store; the client secret is a stored secret (D024) and needs the master key; the API reports when it cannot be saved because no master key is set
- [ ] A pure function takes the environment values and the saved values and returns exactly one state: not configured, saved only, environment only, environment overriding saved (listing which saved values differ or are ignored), or environment incomplete (listing what is missing); the API is built on it; no per-field mixing: if any `REQUESTARR_OIDC_*` is set, all three come from the environment
- [ ] The OIDC authenticator is held behind an atomically swapped snapshot like the metadata providers (D039): a save rebuilds it; a failed rebuild keeps the previous one and reports the error without leaking any value; a configuration that has not passed discovery cannot be enabled; concurrent saves are serialised; in-flight sign-ins finish on the snapshot they started with
- [ ] Admin-only routes: read the state, save settings, run the discovery test (which says what failed), discard saved settings, enable and disable; an unauthenticated route returns only whether OIDC is enabled and its provider name; viewer and requester are refused
- [ ] The redirect address shown to the admin is derived from the public URL
- [ ] Tests: the state function over a table covering every state and every combination of set and missing variables; app-level swap tests with a fake provider (save, failed test keeps the old one, cannot enable without a passing test, enable and disable, environment precedence, startup with none) run with the race detector; role-matrix and contract tests
- [ ] A short decision note is appended to D040 if anything about the design changed while building
- [ ] `scripts/check.sh` passes
