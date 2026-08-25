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

## Normal-auth foundation release gate

Migration `000011_normal_auth_foundation` is supported only for a fresh database or an existing database with an empty `users` table. It changes user identity from a globally unique OIDC subject to an issuer-and-subject pair, but earlier rows have no trustworthy issuer provenance. The migration deliberately rejects legacy users transactionally and does not infer, default, or backfill an issuer. Do not release this build against an installation containing normal users; use a separately reviewed migration/export path when one is approved. Production migrations remain forward-only, and the v11 down migration is limited to empty local/test recovery.

## Dirty-v20 recovery release gate

The certified `maintenance recover-dirty-v20` procedure is a narrowly scoped,
fail-closed operator action. It requires the exact dirty-v20 rollback state,
resets history to truthful clean v19, and replays forward under one advisory
lock. Releases containing it must retain migrations 20–23 unchanged and include
[`000024_source_configuration_orphan_audit_validation.up.sql`](../db/migrations/000024_source_configuration_orphan_audit_validation.up.sql:1).
