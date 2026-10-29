# 02: Source kind registry (internal/sources)

**What to build:** Create internal/sources as pure data plus lookup: per Source kind the slug, source ID, media types, ordered Credential fields (name, plain or secret, env var) and pairing rule. Replace the kind switches and maps in setup/draft.go (Target.Valid, LibrarySource, Settings.Validate wantToken), setup/service.go (decorate, defaultLibraries, normalizeLibraries, defaultSourceMediaTypes), setup/checks/checker.go (library() switches), app/app.go (SourceRef literals, Runtime maps), app/secrets.go (secretSourceURLs), config/config.go (pairing blocks), httpapi/setup.go (field table), and the inventory adapters' SourceID consts. Navidrome is two Credential fields, with no special case. Serve the registry from the API (openapi.yaml first); delete the web copies (media-type defaults in SetupDraftPage.tsx, libraryCredential, kind lists in api.ts).

**Blocked by:** 01

**Status:** done, with the gaps listed under Progress

- [ ] Adding a Source kind edits internal/sources plus its inventory adapter only
- [ ] No string match on jellyfin/plex/romm/navidrome outside internal/sources and the adapters (grep check)
- [ ] One contract test per registered kind proves config, draft, check, opstatus and the web payload agree
- [ ] Web wizard renders from the served registry; TS copies removed
- [ ] sources imports no HTTP, DB, provider or config code

## Progress

Done: `internal/sources` (kind, ID, media types, ordered Credential fields, URL env), with tests that it matches the four inventory adapters and the secrets registry. Adapter `SourceID` consts now alias `sources.*ID`. setup (Target.Valid, LibrarySource, Validate, RequiredTargets, decorate, defaults), config (FromEnv and pairing rules, same messages), app (construction loop, SourceRefs, Runtime maps, secret fields and unreadable handling), the checker (credential lookup and a probe table) and the `httpapi` required-fields table now loop over the registry. `GET /admin/setup/source-kinds` serves it; the wizard's Libraries step renders from it and the TS media-type defaults and `libraryCredential` are gone. Go, integration and all 152 Playwright tests pass.

Behavior changes: Navidrome's unreadable-secret note now appears on its Source row like the other three (the separate "provider" entry is removed); an unreadable library secret holds the whole Source off by one rule.

Gaps (a grep for the slugs still finds these):
- One switch per accessor remains where a struct field must be named: `config.LibraryURL` / `LibraryCredential`, `setup.LibrariesSettings.Source`, `app.newInventorySource` (constructors), and the `checks.libraryProbes` table (probe path, headers, response check).
- `checks.bootCredential` and `setup.Runtime.libraryUsername` / `LibrarySecrets` keys (`<slug>Password`) still name kinds; ticket 03 removes them with the checker's boot secrets.
- The draft JSON keeps its four fixed `libraries.*` keys and `tokenEnv`/`username`/`passwordEnv` fields so stored drafts and the wire format do not change. Credentials are not a free-form list on the wire.
- `web/src/storedSecrets.ts` still lists the Source secrets for the Settings page; it goes with ticket 03.
