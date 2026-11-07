# 04: OIDC scopes are a setting

**What to build:** The operator can choose which OIDC scopes Requestarr requests, from Settings, Authentication, with the change applying at once. New setups ask for the `groups` scope by default; an existing setup keeps exactly what it asks for today. (Spec stories 33-37, 47-48; Implementation Decisions: scopes.)

**Blocked by:** None (can start immediately)

**Status:** done

- [x] A scopes setting is stored with the OIDC settings; `openid` is always requested whatever is saved, including an empty value
- [x] A migration stores `openid profile email` for an existing OIDC configuration so an upgrade changes nothing about the sign-in request (integration test from a database with a saved configuration)
- [x] A new setup defaults to `openid profile email groups`
- [x] The saved scopes are the ones sent in the authorization request (handler test with a fake provider)
- [x] The Test button reads the provider's advertised scopes and shows a warning, not a failure, when `groups` is requested but not listed; it still passes
- [x] The scopes field applies at once without a restart and is read-only with the usual message when the environment configures OIDC
- [x] Playwright with axe at desktop and phone widths covers the field, the warning and the read-only state
- [x] The OpenAPI contract is updated; `scripts/check.sh` passes

## Comments

- Branch `feat/four-roles-04`, migration `000039_oidc_scopes`. Scopes are a space-separated string on the OIDC settings (`Settings.Scopes`, stored in the existing `authentication.oidc` JSON), normalised on save: duplicates removed, `openid` always first, so an empty value saves as `openid`. A stored value without scopes (and the environment) requests `openid profile email` via `authsettings.ScopeList`.
- Criterion 1: `internal/authsettings/scopes_test.go` (normalisation, empty, unsafe characters refused) and `internal/auth/oidc/provider_scopes_test.go` (nil/empty/missing/duplicate all give a leading `openid`).
- Criterion 2: `TestOIDCScopesMigrationKeepsTheExistingRequestIntegration` ran against real PostgreSQL (`requestarr_t04`): a saved configuration gets `openid profile email`, other fields untouched, chosen scopes never overwritten, nothing created when none is saved, down removes it.
- Criterion 3: `TestANewSetupDefaultsToRequestingGroups` and the Playwright new-setup test (field prefilled with `openid profile email groups`).
- Criterion 4: `TestAuthorizationRequestSendsTheConfiguredScopes` builds the real client against an httptest discovery server and reads the `scope` parameter of the authorization URL; `authsettings` tests show the saved list reaches the provider factory. The fake-provider callback in `internal/auth` does not look at scopes, so the check is at the real adapter instead.
- Criterion 5: `TestTestWarnsWhenGroupsIsRequestedButNotAdvertised` (warn, listed, provider lists nothing, groups not requested), the handler test for the `warnings` array, and Playwright (passing Test shows the warning as a status, no alert).
- Criterion 6: `Save` and `SetEnabled` publish through the same atomic snapshot as the other fields (no restart); read-only under the environment (`TestEnvironmentConfigurationShowsTheScopesItRequests` and Playwright: the field is `readonly`, "Set by environment.").
- Criterion 7: Playwright with axe at 1440 and 390 for the field, the warning and the read-only state.
- Criterion 8: `api/openapi.yaml` updated (`scopes` on the settings and update schemas, `warnings` on the test response); `scripts/check.sh` result is recorded below.
- Note for later tickets: `authsettings.Test` now returns `([]string, error)`; `Values` gained `Scopes`. Environment configuration keeps `openid profile email` (no env variable for scopes was asked for).
- scripts/check.sh passed (fmt, test, vet, PostgreSQL integration on requestarr_t04, tsc, 325 Playwright tests).
