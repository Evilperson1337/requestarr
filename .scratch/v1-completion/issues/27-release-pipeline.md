# 27: Release pipeline

**What to build:** A release build produces binaries and a container image that include the UI, and the release path is verified.

**Blocked by:** 03 Rename default branch to main, 04 AGPL-3.0 license and real module path

**Status:** done

- [x] GoReleaser builds the web assets or the dependency is enforced
- [x] Node versions match between CI and the Dockerfile
- [x] Docker build, snapshot and release-check are run and the results recorded
- [x] The container starts and serves the UI
