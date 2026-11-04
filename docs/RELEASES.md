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

Not done: the tag-triggered `release.yml` itself has not run (it needs a pushed `v*` tag), and no image is published to a registry; see "Implemented but unconfirmed" below.

## Supply chain and container policy (D023, D034)

`.github/workflows/release.yml` runs only for a pushed `v*` tag and has two jobs. `scripts/check-release-config.sh` (run by `make release-check` and CI) statically checks that the configuration keeps to this policy.

- **Container image:** `ghcr.io/evilperson1337/requestarr` for `linux/amd64` and `linux/arm64`, built by `docker/build-push-action` from the repository `Dockerfile` (distroless static, non-root, UI embedded, migrations applied at start, `healthcheck` subcommand as the HEALTHCHECK). The Go stage cross-compiles on the build platform, so no emulation is needed. Tags: `vX.Y.Z` for every tag; `vX.Y` and `latest` only for stable tags (no `-` suffix). A pre-release such as `v0.1.0-rc.1` gets only its exact tag, so a release candidate never moves `vX.Y` or `latest`. Nothing is built for publishing from a branch or pull request.
- **SBOM:** archives get an SPDX JSON SBOM (Syft through GoReleaser `sboms`, attached to the GitHub release). The image carries a BuildKit-generated SPDX SBOM attestation.
- **Provenance and signing:** archives, `checksums.txt` and the image get GitHub build-provenance attestations; the image also has BuildKit `mode=max` provenance and a keyless cosign signature (GitHub OIDC identity).
- **Dependencies:** Go and npm dependencies are pinned (`go.sum`, `package-lock.json`, `npm ci`); runtime base images are pinned by tag and updated deliberately.

### Verifying a release image

```bash
IMG=ghcr.io/evilperson1337/requestarr:v0.1.0

# Keyless signature, tied to this repository's release workflow
cosign verify "$IMG" \
  --certificate-identity-regexp '^https://github.com/evilperson1337/requestarr/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# Build-provenance attestation
gh attestation verify "oci://$IMG" --repo evilperson1337/requestarr

# SPDX SBOM and BuildKit provenance
docker buildx imagetools inspect "$IMG" --format '{{ json .SBOM }}'
docker buildx imagetools inspect "$IMG" --format '{{ json .Provenance }}'
```

Archives: `gh attestation verify requestarr_<version>_linux_amd64.tar.gz --repo evilperson1337/requestarr`, and compare against `checksums.txt`.

### Local snapshot

`make snapshot` builds both architectures of the image from the repository Dockerfile (`make snapshot-image`, tag `requestarr:snapshot`, not pushed; it needs a Docker engine with the containerd image store or a buildx builder) and runs `goreleaser release --snapshot --clean --skip=sbom` (SBOMs need Syft, which only the release workflow installs).

### Implemented but unconfirmed

Verified locally (2026-11-04): `make release-check`; `make snapshot` produced linux/amd64 and linux/arm64 images (non-root user, health check present) and the six archives. **Not confirmed until a real tag is pushed to GitHub** (ticket 08, first pre-release): the workflow running at all, the push to GHCR, the tag set produced by `docker/metadata-action`, the cosign signature, the attestations, the image SBOM and GoReleaser's SBOM step. Expect to iterate on the workflow against `v0.1.0-rc.1`. Action versions are floating major tags and are untested.

Before the first public release:
1. ~~choose a Requestarr license~~ (done: AGPL-3.0, D013);
2. ~~pin production frontend dependencies and commit a valid lockfile~~ (done: `web/package-lock.json`, `npm ci` everywhere);
3. ~~finalize embedded frontend build strategy~~ (done: built in every pipeline, not committed);
4. SBOM/provenance/container publishing: written in `release.yml` (D023), unconfirmed until a tag is pushed;
5. ~~document migrations/backups and supported upgrade path~~ (done: [upgrade and backup](UPGRADE_AND_BACKUP.md)).
