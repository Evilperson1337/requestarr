# Requestarr

> **Working title.** Requestarr is a self-hosted shared request and notification portal for movies, TV, music, and games.

Requestarr deliberately stops at **request management, availability reconciliation, and notification**. It does **not** download, acquire, import, rename, or manage media. Users submit a supported metadata URL, Requestarr resolves the item, records one shared request, administrators approve or deny it, connected libraries are periodically reconciled, and subscribed users are notified when the item becomes available.

## V1 at a glance

- Link-based requests (name search is planned for V1.x, D032): **TMDB** (movies/TV), **MusicBrainz** (albums/songs), **IGDB** (games).
- One shared request per canonical item; the original requester is automatically subscribed.
- Other users can choose **Notify Me** on an existing request without creating duplicates or votes.
- Games dynamically expose the platforms reported by IGDB. Users may choose **Any** or multiple acceptable platforms; any selected platform satisfies the request.
- Shared **Requests** screen. `My Requests` is only a filter, never a private section.
- Required **OIDC** authentication, with **Keycloak** as the primary tested/documented provider.
- New OIDC users start as viewers: they can browse and use **Notify Me**; an administrator must promote them to requester to allow request creation. Existing roles are preserved on later logins.
- In-app notifications plus **Apprise API** integration.
- Sources of truth for availability:
  - Movies/TV: Plex and/or Jellyfin
  - Music: Plex, Jellyfin, and/or Navidrome/OpenSubsonic
  - Games: RomM
- First-run setup wizard for OIDC, metadata providers, libraries, notifications, and request policy.
- Local normalized PostgreSQL state, background synchronization, stale-while-revalidate behavior, and targeted request reconciliation.

## Architecture

```text
              TMDB        MusicBrainz        IGDB
                \             |              /
                 +------ Metadata resolvers -+
                              |
                              v
                        Canonical item
                              |
                       Request workflow
                              |
                    PostgreSQL local state
                         /           \
                        /             \
             request events         inventory
                    |                  ^
             notifications             |
              /          \        sync workers
          in-app       Apprise      /   |   |   \
                                  Plex JF Navi RomM
```

Metadata providers answer **“What is this?”**. Library integrations answer **“Do we have it?”**. Requestarr answers **“Has it been requested, who is subscribed, and is it now satisfied?”**.

## Repository layout

```text
cmd/requestarr/             application entry point
internal/                   Go application/domain/integration boundaries
web/                        React + TypeScript client
api/                        OpenAPI contract
config/                     example configuration
db/migrations/              PostgreSQL schema
prompts/                    phased AI implementation prompts
docs/                       product/architecture/security/UX documentation
references/mockups/final/    authoritative UI decision mockups
references/source/           reference manifests/fetch instructions
scripts/                    development/reference helper scripts
.devcontainer/              VS Code Dev Container
.vscode/                    debug/tasks/settings
.github/workflows/          CI and release workflows
```

## Quick start for development

```bash
cp .env.example .env
make dev-up       # start Requestarr, PostgreSQL, Keycloak, and Apprise via compose.dev.yaml
make test
```

The Compose stack runs Requestarr and PostgreSQL as separate containers; the database is persisted in the `requestarr-postgres` volume. Requestarr waits for PostgreSQL's health check, applies migrations at startup, and is available at `http://localhost:8080`. Run `docker compose -f compose.dev.yaml down` to stop the stack; the database volume is preserved. Use `docker compose -f compose.dev.yaml down -v` only when intentionally deleting local database data.

Before starting the stack, configure valid OIDC issuer/client values and a random `REQUESTARR_SESSION_HMAC_SECRET` of at least 32 bytes in `.env`. The OIDC issuer must be HTTPS and reachable from the Requestarr container; the example HTTP Keycloak issuer is not accepted by the current runtime config. `docker compose` reads `.env` for interpolation, but the example placeholders are not production credentials. To run only PostgreSQL and the supporting services, use `docker compose -f compose.dev.yaml up -d postgres keycloak apprise`.

`GET /healthz` is a dependency-free liveness probe; `GET /readyz` additionally checks the database and returns `503` while it is unreachable. For the full development environment, open the repository in its Dev Container.

For initial administrator provisioning, see [`docs/OIDC_AUTH.md`](docs/OIDC_AUTH.md): operators run `requestarr setup-token --issuer <issuer> --subject <subject>`, then the matching authenticated OIDC user claims it once.

After claiming admin, use **Setup** for the resumable draft (General through Review) with explicit
connection checks. Settings remain inactive and secrets stay in the
environment; **Finish** completes the wizard once every required check passes. See the [setup guide](docs/SETUP_WIZARD.md), [upgrade and backup notes](docs/UPGRADE_AND_BACKUP.md) and [project status](docs/research/PROJECT_STATUS.md).

The frontend lives under `web/`:

```bash
make web-install
make web-dev
```

## Build and release

The shared request-list API supports combined title/status/media filters,
subscription-based My Requests, sorting, and stable cursor pagination. See the
[request queue API guide](docs/REQUESTS_API.md) for parameters and snapshot limits.
The [Requests page guide](docs/REQUESTS_UI.md) covers bookmarkable controls,
paging, Notify Me, leaving, and administrator cancellation.

```bash
make build
make snapshot
make release-check
```

Releases are defined by `.goreleaser.yaml`. GoReleaser v2 is expected; run `goreleaser check` before publishing. The project, development container, CI, and production image all target Go 1.27.

## Reference code

`references/source/manifest.yaml` records the upstream projects that informed Requestarr. Use:

```bash
./scripts/fetch-references.sh
```

or on Windows:

```powershell
./scripts/fetch-references.ps1
```

External source trees are deliberately excluded from this repository. The fetch scripts populate ignored local checkout directories and preserve upstream license files. See `references/source/README.md`.

## Start here

1. Read `docs/PROJECT_VISION.md` and `docs/V1_SCOPE.md`.
2. Read `AGENTS.md` before using an AI coding agent.
3. Review `docs/DECISIONS.md`; do not silently reverse accepted decisions.
4. Use the prompts in `prompts/` in numeric order.
5. Treat the images in `references/mockups/final/` as UX decision references, not pixel-perfect specifications.

## License

Requestarr is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0); see `LICENSE.md` and decision D013 in `docs/DECISIONS.md`. Reference projects keep their own licenses and do not apply to Requestarr.
# Production UI build

`make build` runs the Vite production build before compiling the Go binary; Node/npm are required locally. The Docker build performs the same ordering in a Node builder stage and embeds the resulting assets in the Go binary. Browser routes fall back to the SPA, while `/api/` and `/auth/` routes remain server-owned. Development can continue to use `make web-dev`.

This build does not imply V1 completeness. Library synchronization and authoritative availability, native/external notifications, and the complete persistent setup wizard remain outstanding. Configure the required PostgreSQL and OIDC deployment environment according to the setup documentation; no availability is fabricated while library adapters are absent.
