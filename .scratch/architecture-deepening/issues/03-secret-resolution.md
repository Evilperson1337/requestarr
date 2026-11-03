# 03: Resolve secrets once; delete the shims

**What to build:** internal/secrets exposes a resolver: for each Credential field (from sources) and TMDB, IGDB and Apprise, return value, Secret source (environment, stored, shadowed) and an unreadable flag. app calls it once at boot. secrets.Service implements the interfaces httpapi and setup need; delete secretsAPI and setupWithSecrets. Remove the setup.*Env constants and env literals in service.go in favour of registry lookups. The checker's only secret dependency becomes the resolver (delete its seven secret fields and the boot-value fallback). secrets stays free of config/setup imports. Environment still wins; D024 unchanged.

**Blocked by:** 02

**Status:** done, with the gaps listed under Progress

- [ ] One table-driven test: environment, stored, shadowed, unreadable for every credential
- [ ] Adding a secret edits the registry only
- [ ] Checker tests use a fake resolver; per-integration secret test slices are consolidated
- [ ] No behavior change visible in the Review step or GET /admin/secrets

## Progress

Done: the checker's only secret dependency is the resolver (`checks.Config.Secrets`); its TMDB, IGDB, library and boot-value fields, `bootCredential` and the boot-flag fallback are deleted, and `checker.secret` has one path. `secrets.Static` stands in for the resolver in tests. `setup`'s `*Env` names are read from the secrets and Source kind registries instead of literals. `resolveSecrets` returns the unreadable notes, so boot resolves once and `app.Run` no longer calls `Status` a second time. `secretsAPI` is deleted; `secrets.Service` is embedded directly, with a compile-time check that it satisfies `httpapi.SecretsService`. Go, integration and 152 Playwright tests pass.

Behavior change: a check no longer passes on a boot flag alone (`Runtime.TMDBTokenConfigured`, `IGDBSecretConfigured`, `AppriseKeyConfigured`); it needs a value from the resolver. The two flag-only failure tests were removed. Production already wires the resolver, so nothing changes at runtime.

Gaps:
- `setupWithSecrets` stays: the router finds the secrets routes by type assertion on the wizard. Ticket 06 replaces it with an explicit dependency.
- `web/src/storedSecrets.ts` stays: Settings renders from it, and moving it to `GET /admin/secrets` means rewriting mocks in three Playwright specs. Its labels already differ from the server's ("TMDb" vs "TMDB").
- `setup.Runtime` still carries `TMDBTokenConfigured`-style flags and `LibrarySecrets` with `<slug>Password` keys; they feed only the Environment listing in `decorate`. Ticket 04 builds the Runtime from the resolved result.
- `secrets.Resolver` as one boot-time value (resolved Config plus status) was not introduced; `resolveSecrets` plus `Status` already serve it, and ticket 04 is where it gets consumed.

## Follow-up (done)

`web/src/storedSecrets.ts` is deleted. Settings fetches `GET /admin/secrets` and renders a field per secret using the server's label. The registry label for the TMDB token is now "TMDb read token", matching the wizard, so the two no longer disagree. The Playwright mocks for Settings carry labels. `setupWithSecrets` is gone (ticket 06). The `setup.Runtime` flags and `LibrarySecrets` keys are gone: it now carries `SecretsConfigured` (by secret name) and `LibraryUsernames` (by Source slug), filled in `app.Build` from the resolved Config.
