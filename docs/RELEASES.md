# Build / Release

The build system uses Make for local ergonomics and GoReleaser v2 for cross-platform release artifacts.

- `make build` produces a local binary.
- `make snapshot` uses `goreleaser release --snapshot --clean`.
- `make release-check` runs `goreleaser check`.
- tags matching `v*` trigger `.github/workflows/release.yml`.

Before the first public release:
1. choose a Requestarr license;
2. pin production frontend dependencies and commit a valid lockfile;
3. finalize embedded frontend build strategy;
4. add SBOM/provenance/container publishing policy;
5. document migrations/backups and supported upgrade path.
