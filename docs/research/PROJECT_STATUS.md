# Requestarr Project Status Assessment

Date: 2026-10-21. Method: read-only investigation of this repository (HEAD `696d607` on `master`) plus local command runs. Nothing external was consulted. Live PostgreSQL, Keycloak, TMDB/IGDB/MusicBrainz, Jellyfin and Apprise were NOT exercised; claims about them rest on code and tests only.

## 1. Verdict

**Not ready for V1 or any release.** The repo is no longer a scaffold (CLAUDE.md is stale on this point), but it is a working vertical-slice prototype. It implements roughly the M1 identity-and-request loop, Jellyfin movie/TV availability, and in-app plus Apprise notifications. Three of the four required library sources, the setup wizard, the settings and ops UI, a real job system, request search/filter/sort, and the release path are missing.

**Estimated V1 completeness: about 35-40%.** This is a judgement, not a measurement. Of the 20 required V1 scope bullets in section 4, 3 are done, 8 partial and 9 missing or stub-only. The repo's own `docs/IMPLEMENTATION_PLAN.md` (lines 3-14) says "M1 and V1 are not complete" and "Do not describe this repository as V1-complete."

## 2. Commands actually run (2026-10-21)

| Command | Result |
|---|---|
| `make fmt` | Ran, and `git status --short` was clean afterwards (no formatting drift). |
| `make test` | PASS. Every package with tests reports `ok`. Packages with `[no test files]`: `app`, `buildinfo`, `inventory`, `jobs`, `igdb`, `tmdb`, `setup`, `store`, `db/migrations`. |
| PostgreSQL integration tests | **Not run.** `internal/store/postgres/postgres_integration_test.go:1` has `//go:build integration` and skips without `REQUESTARR_TEST_DATABASE_URL` (lines 29, 19, 82). The default `make test` therefore exercises none of the SQL, migrations or transactions. I had no database, so the repo's claim that the integration suite passed (`docs/IMPLEMENTATION_PLAN.md`) is unverified by me. |
| `make vet` | PASS, no output. |
| `make web-check` | PASS (`tsc -b`, `web/node_modules` present). |
| `make build` | PASS. It runs `web-build`, copies the output to `internal/webui/dist`, and builds `bin/requestarr`. `git status` stayed clean because the build outputs are gitignored. |
| `make snapshot`, `make release-check`, Docker build, `make dev-up`, running the binary | **Not run.** `docker` and `goreleaser` are on PATH, but I did not run them. |

## 3. What is actually true now vs. the docs

- **CLAUDE.md is stale.** It says "most `internal/` packages are single-file stubs". About 6,200 lines of Go exist under `internal/` and `cmd/`. `STATUS.md` at the repo root ("Scaffold Validation Status", 2026-08-12) is also stale. `docs/IMPLEMENTATION_PLAN.md` (2026-10-20) is the accurate self-assessment.
- **Real, tested code:**
  - OIDC login, session and CSRF: `internal/auth/handler.go` (275 lines), `internal/auth/oidc/provider.go`.
  - One-time admin bootstrap: the `requestarr setup-token` CLI in `cmd/requestarr/main.go`, plus `POST /setup/claim-admin`.
  - Resolvers: `internal/providers/metadata/{tmdb,musicbrainz,igdb}/resolver.go`.
  - Requests service, subscriptions, transitions, audit: `internal/requests/service.go`, `internal/store/postgres/store.go`.
  - Jellyfin sync: `internal/inventory/jellyfin/client.go`, `internal/app/app.go` (`syncJellyfin`, 15-minute ticker).
  - Notifications: durable outbox plus worker (`internal/notifications/worker.go`) and the Apprise client (`internal/notifications/apprise/client.go`).
  - Admin user and role management in `internal/httpapi/router.go`.
  - 13 migrations in `db/migrations/`.
- **Still stubs or absent:**
  - `internal/inventory/plex`, `navidrome`, `romm` each contain only a README.md (no Go).
  - `internal/jobs/scheduler.go` is a 14-line interface.
  - `internal/setup/state.go` is a 20-line step enum, unused by any wizard logic.
  - `internal/store/store.go` is 5 lines.
- **Frontend:** `web/src` is only 689 lines in total. It is embedded into the Go binary via `internal/webui` (embed.go, handler.go) and the Dockerfile builds it. CLAUDE.md saying it is "not yet wired into the Go binary" is stale.

## 4. Per-scope-item status (docs/V1_SCOPE.md "Required")

| V1 scope item | Status | Evidence |
|---|---|---|
| Go service + React/TS UI + PostgreSQL | **Partial** | The stack exists and builds end to end (`make build` passed). The UI is a thin prototype. |
| First-run wizard with resumable draft config | **Stub / missing** | `web/src/pages/SetupWizardPage.tsx` is only an admin-claim form. Its text says "This page does not configure OIDC or edit server settings". All configuration is environment-only (`internal/config/config.go`). `internal/setup/state.go` is an unused enum. No draft persistence and no connection validation. |
| OIDC-only auth, Keycloak tested and documented | **Partial** | Auth code and `internal/auth/handler_test.go` exist; `compose.dev.yaml` runs Keycloak; `docs/OIDC_AUTH.md` is 43 lines. No live Keycloak run is claimed (`docs/IMPLEMENTATION_PLAN.md`). Group-to-role mapping appears only in `config/requestarr.example.yaml`; the plan says new users default to viewer and the config file is not consumed. |
| Roles (viewer/requester/admin) | **Partial** | Admin role change in `router.go:167`, and the UI toggles only requester/admin (`SettingsPage.tsx`). The viewer role exists as a default but the UI never assigns it, and the "viewer cannot create requests" enforcement was not verified. |
| URL resolver registry: TMDB movie and TV | **Done (unit-level)** | `tmdb/resolver.go`, `metadata/metadata_test.go`. The `tmdb` package has no test files of its own. |
| URL resolver: MusicBrainz release-group and recording | **Done (unit-level)** | `musicbrainz/resolver.go`, `resolver_test.go`. |
| URL resolver: IGDB games | **Partial** | `igdb/resolver.go` has no test file and was not exercised live; platform parsing is at lines 57-79. |
| Universal resolved-item modal | **Done** | `web/src/components/RequestModal.tsx` (25 lines; its UI is untested). |
| One request row per canonical item | **Partial** | Migrations 000002/000005 and `postgres_integration_test.go` target this; not verified here because integration tests were not run. |
| Shared Requests page with search/filter/sort and My Requests | **Partial** | `RequestsPage.tsx` has a shared list and a bookmarkable `?mine=true` filter. There is **no search, status filter, sort or pagination**. `GET /requests` accepts only `mine` (`router.go:213-232`), and the plan admits pagination is absent. |
| Requester auto-subscribed | **Done (by design)** | `subscriptionSource` 'requester' (`api.ts`, `RequestsPage.tsx`). Not verified live. |
| Notify Me / unsubscribe | **Done** | `router.go:293` (PUT/DELETE subscription), button in `RequestsPage.tsx`. |
| Game platform selection from IGDB (Any first, OR) | **Partial** | The UI has Any plus checkboxes (`RequestModal.tsx`). `ValidatePlatformSelection` is in `domain/request.go`. Platforms come from the resolved item (IGDB). However the RomM-side game matching does not exist, so OR semantics are never exercised against a library. |
| Request states (requested, approved, denied, available, cancelled) | **Partial** | The enum is in `domain/request.go:6-12`. Admin approve/deny exists (`router.go:251`, UI). **No UI or endpoint for cancel**; the `available` transition is not wired to inventory as far as I could find (see reconciliation row below). |
| Manual admin state actions with audit events | **Partial** | `POST /requests/{id}/transition` with expectedVersion and operationId (idempotent), migration 000008. The UI offers only Approve/Deny on `requested` items. |
| Metadata cache / local catalog | **Partial** | Catalog records are persisted (migration 000005, `store.go`). No TTL or refresh logic was seen. |
| Library sources: Plex, Jellyfin, Navidrome, RomM | **Partial: 1 of 4** | Jellyfin only (`internal/inventory/jellyfin/client.go`, 155 lines). Plex, Navidrome and RomM are READMEs only. Jellyfin is configured via environment variables (`app.go`), and matching is TMDB IDs for movie/TV only (`reconcile/service.go:exactExternalID`). Music and games cannot match. |
| Background source sync into normalized inventory | **Partial** | `app.go syncJellyfin` is an ad hoc goroutine on a 15-minute ticker. There is no persistent job model, overlap prevention or manual "sync now" (`internal/jobs/scheduler.go` is an interface only). Prompt 01 required a persistent job-run model. |
| Automatic reconciliation and availability notification | **Partial** | `reconcile.MatchSnapshot` is positive-only matching. Migrations 000006, 000009 and 000013 (Jellyfin absence) exist, and `RequestsPage` shows an availability observation. Only Jellyfin TMDB IDs are covered. Title/year matches are only "Suggested", with no review UI. Whether positive evidence flips the request status to `available` was not verified here; the UI shows a separate `availability` field. |
| In-app notifications and Apprise | **Partial** | Durable notifications (migration 000010, `store/postgres/notifications.go`, `NotificationsPage.tsx`) and a global Apprise outbox/worker (000011, `notifications/worker.go`, `apprise/client.go`). The plan states delivery is at-least-once and may duplicate. Per-user preferences are deprecated (D006). Apprise needs environment configuration. |
| Settings and operational status (providers/libraries/jobs/logs) | **Stub** | `SettingsPage.tsx` shows Jellyfin sync times and the user/role table only. There is no provider, job or log status, and no settings editing. |
| Preserve artwork shape by media type | **Done** | `ArtworkKind` (`domain/media.go`), `className ... square` in `RequestsPage.tsx` and `RequestModal.tsx`. |

## 5. Per-phase status (prompts 00-10)

Note: each `prompts/*.md` file is only 3-5 lines long. They are directional, not detailed specs.

| Phase | Status | Evidence |
|---|---|---|
| 00 Master context | n/a | Instructions only. |
| 01 Foundation | **Mostly done, one gap** | Config validation (`internal/config`, 200-line test), secret redaction (`store/postgres/redact.go`), graceful shutdown (`app.go`), PG pool and embedded migrations (`store/postgres/migrate.go`), `/healthz` and `/readyz` (`router.go:57-60`). **Missing: the persistent job-run model** that the prompt names. Request IDs and centralized error responses are inconsistent: handlers mix `http.Error` and JSON error bodies (`router.go:213-232`). |
| 02 Setup + OIDC | **Partial** | OIDC login and admin bootstrap are done. The configuration wizard (General to Finish, docs/DECISIONS.md D012) is **not built**. |
| 03 Metadata resolution | **Mostly done** | The three resolvers and `metadata.NewService` are wired in `app.go`. IGDB and TMDB have no dedicated tests. |
| 04 Requests | **Mostly done** | `internal/requests/service.go` and tests, subscriptions, transitions. Missing: search/sort/pagination, cancel. |
| 05 Game platforms | **Partial** | UI and domain validation are done. No library-side platform matching. |
| 06 Library sync | **Partial (1/4)** | Jellyfin only; no job system. |
| 07 Reconciliation | **Partial** | Positive-only movie/TV matching; no music/game keys, and no targeted reconcile pipeline beyond the snapshot persistence path. |
| 08 Notifications + Apprise | **Mostly done** | See the notification row above. The global-route model deviates from the original design and is recorded in D006. |
| 09 UI polish | **Not started / early** | Total frontend is 689 lines, with inline-style-dense one-line components. There are no frontend tests, no search/filter UI and no wizard. Accessibility was not audited (some roles and labels exist). |
| 10 Harden / release | **Not started** | See section 6. |

## 6. Blockers to V1 / release

1. **Library adapters missing:** Plex, Navidrome/OpenSubsonic and RomM do not exist. Music and game availability cannot be established at all. Jellyfin matches TMDB movie/TV IDs only.
2. **No setup wizard or configuration UI.** This is a V1 requirement (D012). Configuration is environment-only and the "wizard" is an admin-claim form.
3. **No job system.** Sync is an unmanaged goroutine in `app.go` with no persistence, retries, manual trigger or ops visibility. Settings and operational status is therefore missing.
4. **Requests page lacks search, filter, sort and pagination** (scope requirement). There is no cancel action.
5. **License undecided.** `LICENSE.md` says "License Pending... Do not publish or redistribute". The module path is the placeholder `github.com/example/requestarr` (`go.mod:1`). `docs/RELEASES.md` lists the license as a pre-release gate.
6. **No end-to-end acceptance.** There is no live Keycloak, provider, library or Apprise run (the plan says so), no browser/E2E tests and no frontend tests.
7. **Release pipeline unverified.** `docs/RELEASES.md` still lists as open: frontend embedding strategy, SBOM/provenance/container policy, and the migration/backup/upgrade doc. The Dockerfile does build the web app and embed it, but the plan notes Docker was last verified before admin management was added. The `.goreleaser.yaml` build config has no step that builds the web assets, so a bare `goreleaser` run depends on `internal/webui/dist` already existing; I did not run it.

## 7. Risks

- **CI misconfiguration:** `.github/workflows/ci.yml` triggers `push` on branch `main`, but the repo's branch is `master` (`git branch -a`). Only pull-request runs fire. The `.github/workflows/release.yml` and docs also assume `main`. The CI `go` job also runs `gofmt -w ... && git diff --exit-code` (fine), and runs integration tests with `-tags=integration` against Postgres 17, so CI is the only place SQL is exercised.
- **Default tests do not cover the database layer.** A green `make test` is misleading: `store/postgres` integration tests (about 700 lines) need the build tag and a DB.
- **Doc drift:** CLAUDE.md, `STATUS.md`, `docs/MANUAL_TESTING.md` ("what is currently implemented: config validation, PostgreSQL startup..., and the frontend build") and `docs/DECISIONS.md` (the D006 paragraph is duplicated; the "Migration 000007 edited in place" entry sits oddly with the append-only rule) disagree with reality. Future agents are likely to misjudge the state.
- **At-least-once Apprise delivery** can send duplicates (admitted in the plan).
- **Dependency pinning:** `web/package.json` pins React 19.3.0, Vite 8.3.0 and TypeScript 7.0.2 exactly, which is good, but the CI uses Node 22 while the Dockerfile uses `node:26`. Go 1.27.1 is required.
- **Secret handling:** env-only secrets (OIDC, Jellyfin API key, Apprise key) are acceptable for V1 per AGENTS.md, but there is no rotation or UI. Security review was not done by me.
- **Thin UI code quality:** dense single-line components, no component tests, and no pagination means large queues will degrade.
- **Single source of truth for Jellyfin** is hardcoded (`jellyfinSourceID` in `app.go`), so multi-source support needs a model change.

## 8. Prioritized next steps

1. **Fix baseline hygiene (small).** Make CI trigger on `master` (or rename the branch). Run the integration suite locally against the compose Postgres and record the result. Refresh CLAUDE.md, `STATUS.md` and `docs/MANUAL_TESTING.md` to reflect current state.
2. **Build the persistent job runner** (prompt 01 leftover): job-run table, no-overlap guard, manual trigger, status endpoint. Move `syncJellyfin` onto it.
3. **Requests page completeness:** server-side search, status/media-type filters, sort, pagination (URL-bookmarkable per AGENTS.md); add the cancel action; show the viewer role correctly.
4. **Library adapters:** RomM (game platform OR-matching), then Plex, then Navidrome/OpenSubsonic. Each needs fixtures and failure-path tests, and they must keep the "failed sync never removes" invariant. Extend `reconcile` to music and game identifiers beyond TMDB.
5. **Verify status transition to `available`** end to end, and add a test that proves `RequestAvailable` triggers notifications.
6. **Setup wizard and settings:** persist draft configuration (resumable), validate external connections, and add provider/library/job/log status pages. This is the largest UI item.
7. **Test coverage gaps:** tests for the tmdb and igdb packages, auth role matrix (unauthorized, viewer, requester, admin), frontend component tests, and a Keycloak E2E smoke test via `compose.dev.yaml`.
8. **Release readiness:** choose a license, rename the module path, wire the web build into GoReleaser (or document the dependency), run `make release-check`, `make snapshot` and a Docker build, and add upgrade/backup docs, SBOM and a container publishing policy.
9. **Accessibility and responsive audit** against `references/mockups/final/` and `docs/UI_UX.md`; refactor the one-line components.

## 9. Not verified

Live OIDC against Keycloak; any call to TMDB, IGDB, MusicBrainz, Jellyfin or Apprise; PostgreSQL integration tests and migrations against a real database; Docker image build and run; `make snapshot` and `make release-check`; accessibility; whether `available` status transitions are driven by reconciliation (only the supporting code and migrations were located, not traced end to end).
