# 04: The compose stack for first testing

**What to build:** A compose file a tester can run on a server to try Requestarr before using the Unraid template: Requestarr and PostgreSQL only (D034). The user brings their own OIDC provider and TLS. All operator values come from an environment file whose example lists every required setting with placeholders and no usable secret. This ticket also creates the deployment-file check that ticket 06 extends.

**Blocked by:** 02 (compose waits on the health check)

**Status:** ready-for-human

- [x] The compose file runs Requestarr and PostgreSQL, with the database on a named volume, Requestarr waiting for the database to be healthy, and its own health check from the image
- [x] Every operator-supplied value (database URL or credentials, public URL, OIDC issuer, client ID and secret, session secret, master key) is read from an environment file; the example file has a placeholder for each and no value that would pass as a real secret
- [x] The Requestarr image tag is pinned to the exact pre-release `v0.1.0-rc.1` through a single variable, never a floating tag
- [x] A script-level check in the style of the existing stack scripts resolves the compose file with `docker compose config` against a fixture environment file without starting containers, and fails if a required setting is missing from the example, a secret has a usable default, or the tag floats (seam 3 of the spec)
- [ ] The check runs in CI
- [x] `scripts/check.sh` passes (run on the integration branch tip, with the PostgreSQL integration tests and Playwright)

## Comments

The check is wired into the CI workflow and passes locally; the CI run itself is confirmed by the first push to GitHub (ticket 08).
