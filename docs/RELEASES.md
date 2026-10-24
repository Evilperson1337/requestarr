# Build / Release

The build system uses Make for local ergonomics and GoReleaser v2 for cross-platform release artifacts.

- `make build` produces a local binary.
- `make snapshot` uses `goreleaser release --snapshot --clean`.
- `make release-check` runs `goreleaser check`.
- tags matching `v*` trigger `.github/workflows/release.yml`.
- `make check-container` (`scripts/check-container.sh`) builds the Docker image, runs it against the dev compose Postgres and Keycloak, and checks it serves the UI, an SPA route and the API.
- `scripts/check-node-version.sh` fails if the Node major differs between the Dockerfile and the workflows (currently 22).

## The embedded UI

The binary embeds the built UI from `internal/webui/dist`. That directory is build output and is **not committed** (only `.gitkeep` is). Every path that builds a binary builds the UI first: the Dockerfile has a Node stage, GoReleaser runs `npm ci` and `make web-build` as `before` hooks, and CI builds it before `go test`. A plain `go build` without `make web-build` embeds an empty directory; `go test` skips the UI test in that case.

## Recorded results

2026-10-24, local run (Docker, GoReleaser v2):

- `goreleaser check`: configuration valid.
- `goreleaser release --snapshot --clean`, run with the UI deleted first: succeeded; hooks built the UI, six archives (linux, darwin, windows x amd64, arm64) were produced and the linux binary contains the built `assets/index-*.js`.
- `docker build`: succeeded (Node 22 web stage, Go 1.27 build stage, distroless runtime).
- `REQUESTARR_POSTGRES_PORT=5433 make check-container`: PASS. The container started, became ready, served `/`, a built asset, `/requests` and `/api/v1/version`.

Not done: the tag-triggered `release.yml` itself has not run (it needs a pushed `v*` tag), and no image is published to a registry; see the list below.

Before the first public release:
1. ~~choose a Requestarr license~~ (done: AGPL-3.0, D013);
2. ~~pin production frontend dependencies and commit a valid lockfile~~ (done: `web/package-lock.json`, `npm ci` everywhere);
3. ~~finalize embedded frontend build strategy~~ (done: built in every pipeline, not committed);
4. add SBOM/provenance/container publishing policy (the release workflow publishes archives only, no container image);
5. document migrations/backups and supported upgrade path.
