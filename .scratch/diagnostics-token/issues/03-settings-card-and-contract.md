# 03: Create and revoke tokens in Settings, and the API contract

Status: ready-for-agent

**What to build:** `GET`, `POST`, `DELETE /admin/diagnostics-tokens`, superuser-only, with a card in Settings, Logs to create (name and lifetime; the value shown once with a copy button), list (name, created, expires, last used) and revoke. The OpenAPI contract gains the bearer scheme and the token-readable marker. (Spec stories 1-4; Implementation Decisions: managing tokens, contract.)

**Blocked by:** 01, 02

- [ ] The three routes are superuser-only, CSRF on change, `no-store`; a token cannot call them
- [ ] The creation response is the only place the value appears; the page says so and clears it when dismissed
- [ ] `TestSuperuserOnlyRoutesAreExactlyThese` and the OpenAPI contract tests are updated; a test ties `x-token-readable` to the route table
- [ ] Playwright with axe at desktop and phone widths covers create, list and revoke
- [ ] `scripts/check.sh` passes

## Comments
