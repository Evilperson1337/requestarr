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

## Supply chain and container policy (D023, proposed)

- **Container image:** published to GitHub Container Registry as
  `ghcr.io/<owner>/requestarr` for `linux/amd64` and `linux/arm64`, built from the
  repository `Dockerfile` (distroless static, non-root). Tags: `vX.Y.Z` and `vX.Y`
  for every release; `latest` only for stable (non-pre-release) tags. The image
  contains the UI and applies migrations at startup. No image is built from a
  branch or pull request for publishing.
- **SBOM:** an SPDX JSON SBOM is generated for every release archive and for the
  image (Syft, through GoReleaser `sboms`) and attached to the GitHub release.
- **Provenance and signing:** release archives, `checksums.txt` and the image get
  GitHub build-provenance attestations; the image is signed keyless with cosign.
  Consumers verify with `gh attestation verify` / `cosign verify`.
- **Dependencies:** Go and npm dependencies are pinned (`go.sum`, `package-lock.json`,
  `npm ci`); runtime base images are pinned by tag and updated deliberately.
- **Status:** this policy is proposed (awaiting maintainer approval) and **not yet implemented in the workflows**:
  `release.yml` currently publishes archives and checksums only. Implementing it
  needs a pushed `v*` tag to verify, so it is listed as a remaining gap, not claimed.

Before the first public release:
1. ~~choose a Requestarr license~~ (done: AGPL-3.0, D013);
2. ~~pin production frontend dependencies and commit a valid lockfile~~ (done: `web/package-lock.json`, `npm ci` everywhere);
3. ~~finalize embedded frontend build strategy~~ (done: built in every pipeline, not committed);
4. ~~SBOM/provenance/container publishing policy~~ (proposed, D023; implementation in `release.yml` still to do);
5. ~~document migrations/backups and supported upgrade path~~ (done: [upgrade and backup](UPGRADE_AND_BACKUP.md)).
