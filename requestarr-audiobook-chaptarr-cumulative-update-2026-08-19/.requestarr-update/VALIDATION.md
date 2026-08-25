# Update validation

Validated while generating this archive:

- The previous Audiobook V1 update was applied to a clean copy of the supplied 2026-08-17 baseline before the Chaptarr refinements were made.
- Go source added by this update was formatted with `gofmt`.
- The Bash reference fetcher passes `bash -n`.
- `references/source/manifest.yaml` parses successfully.
- Chaptarr's current GitHub default/development branch was verified as `develop` when this pack was generated.
- The cumulative patch is tested with `git apply --check` against a clean copy of the supplied baseline.
- The applied result is byte-compared to the intended updated tree for all affected files.

Full Go tests could not run in the artifact environment because the repository requires Go 1.26 and this environment cannot download the requested Go toolchain. Run the repository's normal Go/PostgreSQL/frontend checks after applying the update in the devcontainer or normal development environment.
