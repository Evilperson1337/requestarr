# 02: The container reports health

**What to build:** The Requestarr image reports healthy only when the app is ready, so Unraid and compose can tell a started-but-broken container from a working one. The image is distroless with no shell, `curl` or `wget`, so the binary gains a `healthcheck` subcommand that probes its own readiness endpoint, and the image declares it as its health check.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] `requestarr healthcheck` makes one bounded HTTP GET of the readiness endpoint on the configured listener address (a wildcard address is probed on loopback), exits 0 on a ready response and non-zero on a not-ready response, a refused connection or a timeout
- [x] It prints nothing secret and needs no database, OIDC or secrets configuration of its own
- [x] The Dockerfile declares the subcommand as the image health check with sensible interval, timeout, start period and retries
- [x] Tests run through the program's entry point against a local test server for ready, not ready, nothing listening and a hung server (seam 2 of the spec)
- [x] A built image, started against a database, goes from `starting` to `healthy`, and stays `unhealthy` when the database is unreachable (verified with the existing container check or an extension of it)
- [ ] `scripts/check.sh` passes (not run by the agent; make fmt test vet passed)
