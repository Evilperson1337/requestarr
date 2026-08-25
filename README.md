# Requestarr

> **Working title.** Requestarr is a self-hosted shared request and notification portal for movies, TV, music, games, and planned audiobooks.

Requestarr deliberately stops at **request management, availability reconciliation, and notification**. It does **not** download, acquire, import, rename, or manage media. Users submit a supported metadata URL, Requestarr resolves the item, records one shared request, administrators approve or deny it, connected libraries are periodically reconciled, and subscribed users are notified when the item becomes available.

## V1 at a glance

- Link-first requests: **TMDB** (movies/TV), **MusicBrainz** (albums/songs), **IGDB** (games).
- One shared request per canonical item; the original requester is automatically subscribed.
- Other users can choose **Notify Me** on an existing request without creating duplicates or votes.
- Games dynamically expose the platforms reported by IGDB. Users may choose **Any** or multiple acceptable platforms; any selected platform satisfies the request.
- Shared **Requests** screen. `My Requests` is only a filter, never a private section.
- Required **OIDC** authentication, with **Keycloak** as the primary tested/documented provider.
- In-app notifications plus **Apprise API** integration.
- Sources of truth for availability:
  - Movies/TV: Plex and/or Jellyfin
  - Music: Plex, Jellyfin, and/or Navidrome/OpenSubsonic
  - Games: RomM
- First-run setup wizard for OIDC, metadata providers, libraries, notifications, and request policy.
- Local normalized PostgreSQL state, background synchronization, stale-while-revalidate behavior, and targeted request reconciliation.

### Planned audiobook V1 policy (not implemented)

Audiobooks are an accepted V1 product direction, but no audiobook resolver, configuration, inventory source, schema, API, or UI is implemented in this repository yet. The planned entry point is an Audible URL, identified by its ASIN plus normalized storefront region. Audnexus will provide metadata/enrichment only; Audiobookshelf will be the sole availability authority. Edition and narrator evidence are significant: only exact or high-confidence matches may automatically fulfill a request, while possible matches require review. Ebooks remain V2 work. Chaptarr is research-only and is neither a runtime dependency nor an acquisition integration. See [the Audiobook V1 decision](docs/DECISIONS.md#d019--audiobook-v1-policy-and-future-ebook-compatibility-2026-08-21).

## Architecture

```text
         TMDB  MusicBrainz  IGDB  Audible (planned)
                 \     |       |       /
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
                         Plex JF Navi RomM ABS (planned)
```

Metadata providers answer **“What is this?”**. Library integrations answer **“Do we have it?”**. Requestarr answers **“Has it been requested, who is subscribed, and is it now satisfied?”**.

## Repository layout

```text
cmd/requestarr/             application entry point
internal/                   Go application/domain/integration boundaries
web/                        React + TypeScript client scaffold
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

Requestarr requires Go 1.26 or newer. Go 1.26 is the unified minimum and current toolchain used by the module, development container, CI, production build, and release workflow.

Requestarr requires PostgreSQL at startup. The database URL is read only from the required `REQUESTARR_DATABASE_URL`; it must be an absolute `postgres://` or `postgresql://` URL with a database name. Startup opens one application pool, pings PostgreSQL, and applies embedded forward migrations before opening the HTTP listener.

```bash
cp .env.example .env
set -a; source .env; set +a
docker compose -f compose.dev.yaml up -d postgres
make test
make run
```

The API starts on `http://localhost:8080` by default. `GET /healthz` is dependency-free process liveness. `GET /readyz` returns readiness only after migrations complete and a bounded PostgreSQL ping succeeds. For the full development environment, open the repository in its Dev Container. PostgreSQL, Keycloak, and Apprise API are defined in `compose.dev.yaml`; the devcontainer uses the `postgres` service hostname rather than `localhost`.

### First-run bootstrap session

Before OIDC setup exists, set `REQUESTARR_SETUP_TOKEN` to exactly 32 cryptographically random bytes encoded as canonical unpadded base64url (43 ASCII characters). Generate one without storing it in shell history where practical:

```bash
openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n'
```

The token is environment-only. Requestarr never generates, persists, prints, or returns it. Missing or malformed values do not prevent liveness, readiness, or the public completion-only setup status from starting; an incomplete instance instead returns `setup_locked` from `POST /api/v1/setup/session`. Accepted exchanges create independent server-side sessions with a 15-minute absolute lifetime; PostgreSQL stores only keyed digests.

Production and every non-loopback deployment must configure an HTTPS `REQUESTARR_PUBLIC_URL`. The only HTTP exception is `REQUESTARR_ENV=development` with `localhost`, `127.0.0.0/8`, or `::1`. Production uses the Secure, HttpOnly, SameSite=Strict `__Host-requestarr_setup` cookie. The explicit loopback HTTP mode uses the separate non-Secure `requestarr_setup_dev` cookie. Requestarr does not trust forwarded headers for origin, cookie, or rate-limit decisions; a reverse proxy must preserve the configured public origin and terminate external TLS itself.

The exchange endpoint accepts only an exact-origin JSON request; do not put the setup token in URLs, authorization headers, cookies, logs, or committed environment files. Future session-authenticated setup mutations additionally require `X-Requestarr-CSRF: 1`. Changing the token while setup is incomplete revokes practical use of existing setup sessions but is not a reset workflow. Completed setup remains complete regardless of later token changes.

Production startup migration is forward-only. Existing down migrations are retained for explicit development recovery, but the application never invokes them. Back up PostgreSQL before upgrades and roll back application binaries only when they remain schema-compatible; otherwise restore a tested backup.

Secret-at-rest encryption uses `REQUESTARR_ENCRYPTION_KEY`, which must be exactly 32 new random bytes encoded as canonical unpadded base64url. Generate it with `openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n'`. Never reuse `REQUESTARR_SETUP_TOKEN`. Store the encryption key outside PostgreSQL and back it up together with each database backup. A database containing encrypted rows fails startup before listen if the key is missing, malformed, or incompatible. V1 supports one active key and no online rotation; replacing or losing the key makes existing ciphertext unavailable. An incomplete installation with no encrypted rows may start locked for secret operations.

Migration advisory-lock acquisition is bounded by both golang-migrate's 15-second client guard and PostgreSQL's 15-second session `lock_timeout`. The runner API cannot attach a caller context directly to `Up`; cancellation is therefore graceful between migration files, while each migration operation is independently bounded by a 60-second statement timeout. During execution of any individual migration file, cancellation is enforced by that statement bound rather than immediately by the process context.

The frontend scaffold lives under `web/`:

```bash
make web-install
make web-dev
```

## Build and release

```bash
make build
make snapshot
make release-check
```

Releases are defined by `.goreleaser.yaml`. GoReleaser v2 is expected; run `goreleaser check` before publishing. Builds and releases use Go 1.26, which is also the minimum supported Go version.

## Reference code

`references/source/manifest.yaml` records the upstream projects that informed Requestarr. It does not include research-only material; [audiobook research notes](references/research/AUDIOBOOKS_AND_CHAPTARR.md) retain their own provenance and no-copy framing. Use:

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

No Requestarr project license has been selected yet. `LICENSE.md` intentionally records that decision as pending. Do not assume the licenses of reference projects apply to Requestarr.
