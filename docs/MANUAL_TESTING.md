# Manual Testing Guide

Step-by-step verification of the foundation: config validation, PostgreSQL startup/migrations/readiness, and the frontend build. These steps do not cover the request, OIDC, Jellyfin sync or notification features, which are implemented but have no manual walkthrough here yet (see `docs/IMPLEMENTATION_PLAN.md` for current status). See `docs/TESTING.md` for the broader automated-testing strategy as more of the system is implemented.

## Quick start: full stack in Docker

```bash
make stack-up      # builds/runs app + Postgres + Keycloak; creates a local secrets key and prints first-admin setup details
make stack-up-dev  # same stack, explicitly enables development connection diagnostics in the UI
make stack-logs    # follow logs
make stack-admin-token  # issue first-admin token only if none exists; cannot reprint an existing token
make stack-down    # stop; STACK_PURGE=1 make stack-down also deletes its data
```

Open http://localhost:18080 and sign in as `requestarr-admin` / `requestarr-admin` (Keycloak admin console: http://localhost:8081, `admin` / `admin`). Postgres is published on host port 15432 (`REQUESTARR_STACK_POSTGRES_PORT`). The stack uses host ports 18080 and 8081; Keycloak still conflicts with `make dev-up`, so stop that stack first. Add TMDB/IGDB tokens or library sources through the setup wizard or by extending `compose.test.yaml`.

The stack-only `REQUESTARR_STACK_HTTP_PORT` override controls the listen address,
public URL, printed URL and startup readiness probe together. Set it in `.env`,
the host environment, or with `make stack-up REQUESTARR_STACK_HTTP_PORT=18080`.
An explicit Make value wins over the host environment, which wins over `.env`;
an empty value uses 18080. Ordinary `make dev-up` / `make run` defaults remain 8080.
The imported Keycloak client already allows exact callbacks at
`http://localhost:8080/api/v1/auth/callback` and
`http://localhost:18080/api/v1/auth/callback`. Other ports require registering the
exact callback URL on the existing local Keycloak client first; changing the
port does not register it automatically. Do not add wildcard redirects or
recreate the realm/database. The 18080 default needs no realm edits, token
rotation, secrets-key changes or volume deletion; existing setup state persists.

Both startup commands preserve the database volume and existing local secrets key. Repeating startup skips first-admin token issuance when bootstrap is complete or any token already exists; it never silently rotates or reprints a token. Keep the original token for the claim. If it expires before the claim, explicitly run `make stack-admin-token ROTATE=1`. Database/configuration/issuance failures still fail startup.

Before inspecting bootstrap state, startup waits for the app's public local
`http://localhost:18080/readyz` endpoint (or the resolved stack port override) so migrations can finish. It makes at most
30 probes (one-second connection timeout, two-second total timeout each), with
one second between failed probes: at most approximately 89 seconds. If readiness
never succeeds, startup fails without querying bootstrap SQL or issuing a token.
Explicit `make stack-admin-token` issuance/rotation does not use this startup wait.

### Development connection diagnostics

1. Run `make stack-up-dev` and sign in as an **administrator**. The explicit server flag `REQUESTARR_DEV_LOGS=true` is accepted only with `REQUESTARR_ENV=development`; it defaults off. It is not controlled by the frontend build mode.
2. Open **Setup** (`/setup`, after the first-admin claim, even while the wizard is incomplete) or **Settings** (`/settings`) and expand **Development connection logs**. The panel starts collapsed and only appears when the server advertises the capability to an administrator. Non-admins cannot read diagnostics.
3. For an empty history, expect guidance to run a setup connection check. Re-run a failing/successful check in Setup, then open the panel or press **Refresh logs**. There is no polling: refresh manually after each check. Reading status/logs does not contact providers or run checks.
4. Inspect target, stage (`check`, `request`, `oauth`, `platforms`), outcome, safe code, optional numeric HTTP status, timestamp and duration. These are narrow structured connection diagnostics, **not arbitrary server/container logs or Docker dependency logs**. There are no raw response/error payloads, URLs, credentials, tokens, cookies or authorization headers. Do not paste raw provider responses or tokens into bug reports.
5. At most 200 records are held in server memory and in the panel, oldest first. Restarting Requestarr clears server history. Closing the panel, navigating away or losing authorization clears displayed records; no browser storage is used. A failed refresh shows a generic retry message rather than a raw error body. Records and successful checks **do not mean a runtime integration has been activated**; draft/runtime and restart requirements still apply.
   A 401/403 from Setup stored-secret status, save, or clear (including Review)
   must also remove the panel and cancel any pending diagnostic refresh; a late
   response must not restore records. Secret-action failures show fixed messages,
   never raw error payloads.
6. Run ordinary `make stack-up` to disable diagnostics again, even if `.env` says `REQUESTARR_DEV_LOGS=true`. Compose reconfigures the app with the flag explicitly false, preserving volumes/key. Reload the UI: neither Setup nor Settings should show the panel. Direct Compose use defaults the flag to false but honors explicit environment/`.env` overrides.

With records displayed in Settings, verify that an expired/revoked administrator session returning 401 or 403 from a different Settings action (sync, role change, secret status/save/clear, or suggested-match load/review) immediately removes the diagnostic panel without refreshing logs. Pending diagnostic reads must be cancelled; a late status/log response must not restore the panel for that session.

Library checks validate response media types before parsing: Jellyfin, RomM and Navidrome expect JSON; the Plex setup probe expects XML (both application/xml and text/xml, including charset parameters). A successful HTTP response containing an HTML login/proxy page is classified with the fixed content-type diagnostic category, without retaining the header or body. Non-success HTTP statuses retain their HTTP-status classification.

No Docker socket mount or log sidecar is used. `make stack-logs` remains a separate operator-only terminal workflow and is not connected to this panel. Never enable development diagnostics on a production deployment.

## 0. Prerequisites

- Go 1.27+, Node 22, and either Docker (for `compose.dev.yaml`) or any reachable PostgreSQL 15+.
- If your local `go` is older than 1.27 and can't auto-fetch a toolchain, run `go env -w GOTOOLCHAIN=auto` once (`go.mod` requires Go 1.27.1).

```bash
cd /workspaces/requestarr
cp .env.example .env
```

## 1. Static checks

```bash
make fmt && git diff --exit-code   # expect: no output, no diff (already formatted)
make vet                            # expect: clean, no output
make test                           # expect: all packages "ok" or "[no test files]", nothing fails
```

## 2. Start PostgreSQL

```bash
make dev-up          # starts Postgres/Keycloak/Apprise via compose.dev.yaml
```

If you don't have Docker, point `REQUESTARR_DATABASE_URL` in `.env` at any Postgres 15+ instance instead (user/db `requestarr`/`requestarr` to match the compose defaults, or edit `.env` to match yours).

## 3. Config validation (should fail fast, no partial startup)

```bash
REQUESTARR_DATABASE_URL= go run ./cmd/requestarr
```

**Expect:** immediate exit, `requestarr: invalid configuration: REQUESTARR_DATABASE_URL is required`, no attempt to bind a port.

```bash
REQUESTARR_DATABASE_URL=postgres://x REQUESTARR_LOG_LEVEL=nonsense go run ./cmd/requestarr
```

**Expect:** `REQUESTARR_LOG_LEVEL must be one of debug, info, warn, warning, or error` — and the string `nonsense` must **not** appear anywhere in the output (secret/value-safe error).

## 4. Migrations

```bash
make migrate-up
```

**Expect:** log line `migrations applied`, exit 0.

```bash
docker compose -f compose.dev.yaml exec postgres psql -U requestarr -d requestarr -c '\dt'
```

**Expect:** at least `users`, `catalog_items`, `requests`, `request_subscriptions`, `request_events`, `library_sources`, `inventory_items`, `notification_deliveries`, `setup_state`, `schema_migrations`, plus the tables added by later migrations in `db/migrations/`.

Run `make migrate-up` a second time — **expect:** same success, no errors (idempotent, no duplicate-table errors).

## 5. Run the server and probe it

```bash
make run
```

In another terminal:

```bash
curl -s localhost:8080/healthz | jq        # expect: {"status":"ok","time":"..."} always, HTTP 200
curl -s localhost:8080/readyz  | jq        # expect: {"status":"ok"}, HTTP 200 (Postgres is up)
curl -s localhost:8080/api/v1/version | jq # expect: {"version":"dev","commit":"...","date":"..."}
curl -s localhost:8080/api/v1/setup/status | jq
curl -s localhost:8080/                    # expect: the embedded web UI (HTML shell) when built with the web assets
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/nope   # expect: 404
```

## 6. Readiness under a database outage (the important one)

With the server still running from step 5:

```bash
docker compose -f compose.dev.yaml stop postgres
curl -s -w '\n%{http_code}\n' localhost:8080/readyz    # expect: {"status":"unavailable"} / 503
curl -s -w '\n%{http_code}\n' localhost:8080/healthz   # expect: still {"status":"ok"} / 200 — must NOT go down with the DB
```

Check the server's own logs — the `/readyz` 503 response body and the log line must **not** contain the database password or full connection string (only a redacted `postgres://host:port/db` form, no credentials).

```bash
docker compose -f compose.dev.yaml start postgres
sleep 2
curl -s -w '\n%{http_code}\n' localhost:8080/readyz    # expect: back to {"status":"ok"} / 200, automatically
```

## 7. Graceful shutdown (no hang)

Stop the server from step 5 with `Ctrl+C` (or `kill -TERM <pid>` from another terminal).

**Expect:** the process exits promptly (well under the 10s shutdown timeout) with no goroutine/stack-trace dump. If it hangs, that's a regression — this exact scenario (`pool.Close()` blocking forever) was a real bug caught while building this.

## 8. `-migrate-only` in isolation

```bash
go run ./cmd/requestarr -migrate-only
```

**Expect:** `migrations applied`, exit 0, and it does **not** open port 8080 (confirm with `curl -s localhost:8080/healthz` failing to connect while this runs).

## 9. PostgreSQL integration test suite

```bash
REQUESTARR_TEST_DATABASE_URL=postgres://requestarr:requestarr@localhost:5432/requestarr?sslmode=disable \
  go test -tags=integration ./internal/store/postgres/... -v
```

**Expect:** every test in the suite `PASS`es, including `TestOpenAppliesMigrations` and `TestOpenIsIdempotent`.

Without that env var set:

```bash
go test -tags=integration ./internal/store/postgres/...
```

**Expect:** tests skip cleanly (no failure, no attempt to touch your real `REQUESTARR_DATABASE_URL`).

## 10. Frontend

```bash
make web-install   # npm ci against the committed package-lock.json
make web-check      # expect: clean tsc, no errors
make web-build       # expect: dist/ produced, no errors
make web-dev          # then open http://localhost:5173 and confirm the "Request Something" home page renders
make build             # builds the web app, embeds it in the Go binary and writes bin/requestarr
```

## 11. Full gate, one shot

```bash
bash scripts/check.sh
```

**Expect:** fmt/test/vet all pass, and `web-check` runs (not skipped) since `web/node_modules` now exists.

## 12. Cleanup

```bash
make dev-down
```

## 13. Suggested-match review

1. Create a request for an item whose library entry has the same title and year
   but no matching stable provider ID. Run a successful Source sync.
2. As an admin, open **Settings → Suggested matches**. Check both titles, years,
   Source/library ID, platform (if any), observation time, confidence, and reason.
   Confirm that the request has **not** become available merely because the
   suggestion exists.
3. Accept the match. Confirm that the request becomes available if its platform
   selection is satisfied, and its subscribers get the ordinary availability
   notification. Refresh/resync: the suggestion stays out of the pending list
   and no duplicate notification appears.
4. For a second candidate, dismiss it. Its availability must stay unchanged;
   refresh/resync must not reopen it. Retrying the same decision is a no-op;
   the opposite decision returns a conflict.
5. Repeat with a missing year: the UI must show **Title only · lower confidence**
   and explain the missing year. Different known years, media types, or
   normalized titles must not create a candidate. Stable-ID matches bypass review.
6. Remove a pending library entry and successfully sync, then attempt to accept
   it from an old tab: acceptance must report a stale candidate. A temporary
   Source failure alone must not discard the last-known pending candidate.
7. As a viewer/requester, verify that the list and both review endpoints return
   forbidden; without a session they return unauthorized. Mutations with a
   missing/invalid CSRF token or untrusted Origin must fail without side effects.

Automated PostgreSQL coverage lives in
`internal/store/postgres/suggestions_integration_test.go`; router authorization,
CSRF, and safe errors are covered by `internal/httpapi/suggestions_test.go`.

## 14. Real Unraid server

Automated checks cannot cover a real Unraid host, a real reverse proxy and a real identity provider. Run this checklist against a pre-release tag before a stable release, following [the Unraid guide](UNRAID.md). Record the tag, the date and the result of each line.

- [ ] **Install from the Template Repositories URL.** In Unraid's Docker settings add `https://raw.githubusercontent.com/evilperson1337/requestarr/main/deploy/unraid/requestarr.xml` to Template Repositories, add the container from the template, fill in the fields (use a user-defined network), and apply. The icon and project link show in the Docker tab, the image pulls from the registry, and the tag is the pinned pre-release.
- [ ] **Healthy container.** The Docker tab (and `docker ps`) shows the container as healthy, not just started. Stop PostgreSQL: the container turns unhealthy within a few minutes. Start it again: it recovers. The log shows the migration and readiness lines.
- [ ] **Bad configuration is reported.** With `REQUESTARR_ENV=production`, an `http://` public URL and a malformed master key each stop the container at start with a log line naming the variable.
- [ ] **First-admin sign-in through Keycloak.** With the Keycloak client from the guide: run `docker exec <container> /requestarr setup-token --issuer ... --subject ... --stdout` from the Unraid terminal, sign in at the https public URL, claim the admin with the token, and reach Setup as admin. A second user signs in as a viewer and cannot create a request.
- [ ] **Library over a private `http://` address.** Add a Plex, Jellyfin or similar Source by LAN address (and, on a user-defined network, by container name) over `http://`, save its credential in Setup, restart, and watch a sync complete with items showing as available. An `http://` address on a public DNS name is rejected with a message that says why.
- [ ] **Master key separation.** Restore a database dump into a fresh container with a different master key: the container starts, and saved credentials report "stored secret unreadable" instead of crashing.
