# 04: OIDC scopes are a setting

**What to build:** The operator can choose which OIDC scopes Requestarr requests, from Settings, Authentication, with the change applying at once. New setups ask for the `groups` scope by default; an existing setup keeps exactly what it asks for today. (Spec stories 33-37, 47-48; Implementation Decisions: scopes.)

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] A scopes setting is stored with the OIDC settings; `openid` is always requested whatever is saved, including an empty value
- [ ] A migration stores `openid profile email` for an existing OIDC configuration so an upgrade changes nothing about the sign-in request (integration test from a database with a saved configuration)
- [ ] A new setup defaults to `openid profile email groups`
- [ ] The saved scopes are the ones sent in the authorization request (handler test with a fake provider)
- [ ] The Test button reads the provider's advertised scopes and shows a warning, not a failure, when `groups` is requested but not listed; it still passes
- [ ] The scopes field applies at once without a restart and is read-only with the usual message when the environment configures OIDC
- [ ] Playwright with axe at desktop and phone widths covers the field, the warning and the read-only state
- [ ] The OpenAPI contract is updated; `scripts/check.sh` passes
