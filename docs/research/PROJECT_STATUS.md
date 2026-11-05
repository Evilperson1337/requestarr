# Requestarr project status

Date: 2026-10-24. Replaces the 2026-10-21 assessment (about 35-40% complete, not releasable), which is in git history. Scope reference: [V1_SCOPE](../V1_SCOPE.md). Work was tracked as tickets 01-28 in `.scratch/v1-completion/`.

## Verdict

**Feature-complete for V1 scope against fixtures and mocks; not yet release-ready.** Every V1 "Required" item below is implemented and tested except that settings and operational status lack a log view, but nothing has been run against real third-party services, the release workflow has never run on a tag, and a few quality gaps are open (listed in section 4). Calling this "V1 complete" would overstate what was verified.

## 1. Acceptance run (2026-10-24)

| Check | Result |
|---|---|
| `make fmt test vet web-check` | PASS |
| `make test-integration` (PostgreSQL 17, compose) | PASS, run twice. The earlier Plex music failure was test pollution (a cleanup that ignored a foreign-key error left open music requests that crowded the test's 50-item work list); fixed in `aliases_integration_test.go` |
| `make check-login` (scripted Keycloak login) | PASS against Keycloak 26.3 |
| Frontend tests (`npm test`, Playwright, 59 tests incl. axe audit) | PASS |
| `goreleaser check`, `goreleaser release --snapshot`, Docker build, `make check-container` | PASS (recorded in [RELEASES](../RELEASES.md)) |

Ran with `REQUESTARR_POSTGRES_PORT=5433` because port 5432 was in use on the test machine.

## 2. V1 scope status

| V1 required item | Status | Notes |
|---|---|---|
| Go service, React/TS UI, PostgreSQL | Done | Single binary embeds the UI; built in every release path |
| First-run Get started checklist | Done | After the first-admin claim, a checklist computed from live state links to the Settings that apply each item (D038). It replaced the inactive draft wizard, which applied nothing |
| OIDC-only auth, Keycloak tested | Done | Scripted login passes; dev-only HTTP loopback issuer (D022) |
| Viewer / requester / admin roles | Done | Role matrix tests cover every mutating route |
| TMDB, MusicBrainz, IGDB resolvers | Done (fixtures) | Never run against the live services |
| Resolved-item modal | Done | Keyboard operable (focus trap, Escape) |
| One request per canonical item | Done | Integration-tested |
| Requests page: search, filter, sort, My Requests, pagination | Done | URL-bookmarkable |
| Auto-subscribe requester; Notify Me | Done | |
| IGDB game platforms, Any / OR semantics | Done | Per-platform availability (D021) |
| Request states incl. cancel; admin actions with audit | Done | |
| Metadata cache / local catalog | Done (cache TTL/refresh not re-verified) | No TTL/refresh behaviour was re-verified in this pass |
| Audiobook requests by ASIN, Audiobookshelf source (D031) | Done (fixtures) | Audnexus/Audible resolver, read-only Audiobookshelf adapter, request UI, Library tab; never run against a real Audiobookshelf, Audnexus or Audible; no retry job for unresolved ASINs; title-only (not author) suggested matches |
| Plex, Jellyfin, Navidrome, RomM sources | Done (fixtures) | One adapter contract suite; none run against a real server |
| Background sync into normalized inventory | Done | Persistent runner, no-overlap lease, retry, Sync now |
| Automatic reconciliation and availability notification | Done | Failed syncs never create absence; suggested-matches review |
| In-app notifications and Apprise | Done | Apprise delivery is at-least-once (duplicates possible) |
| Settings and operational status | Mostly done | Providers, Sources, jobs and recent errors shown; **no log viewer** |
| Artwork shape per media type | Done | |

## 3. What changed since the last assessment

Tickets 01-28 delivered: documentation refresh, branch rename, AGPL licence and module path, role/last-admin guards, consistent error shape, availability evidence, request cancel and leave, the persistent job runner and Sync now, the shared adapter contract suite and the Plex, RomM, Jellyfin and Navidrome adapters, suggested matches, request search/pagination and its page controls, role-matrix tests, the setup wizard (all steps and Finish), the operational status view, frontend smoke and accessibility tests, the scripted Keycloak check, the release pipeline fixes, and upgrade/backup and supply-chain documentation. Real bugs found along the way include first sign-in failing with HTTP 500 (non-UUID user id), a wrong dev-realm callback URL, and a stale committed UI bundle that releases would have embedded.

## 4. Remaining gaps (explicit)

1. **No live acceptance.** TMDB, IGDB, MusicBrainz, Jellyfin, Plex, RomM, Navidrome, Audiobookshelf, Audnexus/Audible and Apprise have only been exercised against fixtures/mocks. Real-service runs may surface contract drift.
2. **Provider credentials apply after a restart.** Library connections apply at once; TMDB, IGDB (secret and client ID) and Apprise secrets still apply after a restart (live reload is planned in the UX review).
3. **Request policy has no effect.** The retired wizard recorded which media types skip approval, but nothing read it; every request still awaits approval.
4. **Release not finished.** `release.yml` has never run on a tag. The multi-arch image, SBOM, provenance and signing from D023 are now written into the workflow (see RELEASES.md) but unconfirmed until a tag is pushed, so no image is published yet.
5. **Accessibility not fully closed** (ticket 26 is `ready-for-human`): no manual screen-reader pass, keyboard walk-through only for the request dialog, only two widths tested, and the Requests page and dialog were not compared with the mockups. Known mockup gaps: Home panels, per-library settings cards with Test Connection, notification badge, activity page. See [the audit](../ACCESSIBILITY_AUDIT.md).
6. **No log viewer** in operational status (V1 scope names "logs").
7. **Apprise duplicates** are possible after an ambiguous upstream acceptance.
8. **No security review** was performed, and frontend tests are end-to-end with a mocked API only (no component tests).
9. **Frontend mocks can drift** from `api/openapi.yaml`; nothing checks them against it.

## 5. Suggested next steps

1. Run the stack against real Jellyfin/Plex/RomM/Navidrome/TMDB/IGDB/Apprise instances and fix what breaks.
2. Implement D023 in `release.yml` and cut a pre-release tag to prove the pipeline.
3. Decide whether request policy is V1 or post-V1; either enforce them or narrow the scope text.
4. Finish the accessibility pass by hand and add the log view or drop "logs" from the scope.
5. Security review of auth, setup checks (outbound request handling) and the admin routes.
