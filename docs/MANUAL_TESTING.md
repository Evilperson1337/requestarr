# Manual Testing Guide

Step-by-step verification of the foundation: config validation, PostgreSQL startup/migrations/readiness, and the frontend build. These steps do not cover the request, OIDC, Jellyfin sync or notification features, which are implemented but have no manual walkthrough here yet (see `docs/IMPLEMENTATION_PLAN.md` for current status). See `docs/TESTING.md` for the broader automated-testing strategy as more of the system is implemented.

## Quick start: full stack in Docker

```bash
make stack-up      # app + PostgreSQL + development Keycloak; preserves existing data
make stack-up-dev  # same stack with development connection diagnostics
make stack-down    # STACK_PURGE=1 also deletes data; do not use for password recovery
```

Open http://localhost:18080/login. On a fresh database, read the local
administrator's generated one-time password in the container log, sign in as
`admin`, and choose a new 12–128 character password. Do not publish logs that
contain the initial password. Existing instances keep their users and roles and
do not create a new credential automatically. Keycloak remains available at
http://localhost:8081 (`admin` / `admin`); the fixture OIDC user is
`requestarr-admin` / `requestarr-admin`, initially a viewer unless already
promoted. An existing admin promotes people in Settings, Users.

Recovery check (never use a production database for destructive tests):

```bash
docker compose -f compose.test.yaml exec requestarr /requestarr reset-admin-password
```

Confirm it prints exactly one new password and no hash, the old password and
all old local-account sessions stop working, and the new password requires a
change at next sign-in. With no local account, it must fail clearly without
creating one. Keep command output private. Audit and credential/session updates
are atomic. Password recovery needs only database configuration, not working
OIDC. Open **Get started** after signing in for live setup links.

The stack-only `REQUESTARR_STACK_HTTP_PORT` controls the listen/public/printed
URL (default 18080). An explicit Make value wins over shell, then `.env`; empty
uses the default. Register the exact callback URL with Keycloak for a different
port; do not add wildcard redirects or recreate the realm/database. PostgreSQL
uses host port 15432. Stop conflicting development stacks first. Repeating
startup preserves data and never reprints a password for an existing account.
The OIDC fixture settings of this stack come from the environment, so Settings, Authentication shows them as set by the environment. The real-server checklist for the first-run flow is section 14.

### Development connection diagnostics

1. Run `make stack-up-dev` and sign in as an **administrator**. The explicit server flag `REQUESTARR_DEV_LOGS=true` is accepted only with `REQUESTARR_ENV=development`; it defaults off. It is not controlled by the frontend build mode.
2. Open **Settings** (`/settings`), then **Activity**, and expand **Development connection logs**. The panel starts collapsed and only appears when the server advertises the capability to an administrator. Non-admins cannot read diagnostics.
3. For an empty history, expect guidance to run a connection check. Re-run a failing/successful **Test connection** on a library in Settings, Integrations, then open the panel or press **Refresh logs**. There is no polling: refresh manually after each check. Reading status/logs does not contact providers or run checks.
4. Inspect target, stage (`check`, `request`, `oauth`, `platforms`), outcome, safe code, optional numeric HTTP status, timestamp and duration. These are narrow structured connection diagnostics, **not arbitrary server/container logs or Docker dependency logs**. There are no raw response/error payloads, URLs, credentials, tokens, cookies or authorization headers. Do not paste raw provider responses or tokens into bug reports.
5. At most 200 records are held in server memory and in the panel, oldest first. Restarting Requestarr clears server history. Closing the panel, navigating away or losing authorization clears displayed records; no browser storage is used. A failed refresh shows a generic retry message rather than a raw error body. Records and successful checks **do not mean a runtime integration has been activated**; draft/runtime and restart requirements still apply.
   A 401/403 from a stored-secret status, save, or clear in Settings
   must also remove the panel and cancel any pending diagnostic refresh; a late
   response must not restore records. Secret-action failures show fixed messages,
   never raw error payloads.
6. Run ordinary `make stack-up` to disable diagnostics again, even if `.env` says `REQUESTARR_DEV_LOGS=true`. Compose reconfigures the app with the flag explicitly false, preserving volumes/key. Reload the UI: Settings should not show the panel. Direct Compose use defaults the flag to false but honors explicit environment/`.env` overrides.

With records displayed in Settings, verify that an expired/revoked administrator session returning 401 or 403 from a different Settings action (sync, role change, secret status/save/clear, or suggested-match load/review) immediately removes the diagnostic panel without refreshing logs. Pending diagnostic reads must be cancelled; a late status/log response must not restore the panel for that session.

Library checks validate response media types before parsing: Jellyfin, RomM and Navidrome expect JSON; the Plex connection probe expects XML (both application/xml and text/xml, including charset parameters). A successful HTTP response containing an HTML login/proxy page is classified with the fixed content-type diagnostic category, without retaining the header or body. Non-success HTTP statuses retain their HTTP-status classification.

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

**Expect:** immediate exit, `requestarr: invalid configuration: database is not configured: set REQUESTARR_DATABASE_URL, or set REQUESTARR_DB_HOST, ...` (the database can also be given as separate `REQUESTARR_DB_HOST`, `REQUESTARR_DB_PORT`, `REQUESTARR_DB_NAME`, `REQUESTARR_DB_USER`, `REQUESTARR_DB_PASSWORD`, `REQUESTARR_DB_SSLMODE` settings; the URL wins when both are set), no attempt to bind a port.

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
- [ ] **Bad configuration is reported.** With no variable added by hand (the template sets `REQUESTARR_ENV=production`), an `http://` public URL and a malformed master key each stop the container at start with a log line naming the variable.
- [ ] **First sign-in with the generated password.** Install with the OIDC fields and the Initial Administrator Password left empty. On a fresh database find the log line `Requestarr created the local administrator. Sign in as 'admin' at <public URL>/login ...` (Docker tab, the container's Logs) and read `one_time_password`. Open the HTTPS `/login`, sign in as `admin`, and confirm **Choose a new password** appears before anything else; every other page is refused until you change it. Restart the container: no second line and no second account appears. Repeat once with `REQUESTARR_ADMIN_PASSWORD` set (12 to 128 characters): the line says the password came from the variable and shows none; a shorter value stops the container with a log line naming the variable and not repeating the value.
- [ ] **Configure and test OIDC.** In Settings, Authentication fill in the OpenID Connect card for your real provider (Keycloak: confidential client, client authentication on, standard flow, the exact Redirect address from the page, scopes `openid profile email`). Press **Save OIDC settings**, **Test OIDC connection**, then switch on **Enable OIDC**; the status reads `On · <provider name>` with no restart. A private window now offers **Sign in with** the provider, and an **Administrator sign-in** link.
- [ ] **Promote an OIDC person.** Sign in through OIDC as someone new: they are a viewer (or the chosen default role). As the local administrator open Settings, Users and promote them; they need no identity-provider ID. Viewers and requesters must be denied role changes. The local administrator row is locked as always administrator.
- [ ] **Break OIDC on purpose.** Save a wrong issuer or stop the identity provider, then confirm `/login` still signs in the local administrator and Settings, Authentication reports the problem rather than locking you out. Restore it.
- [ ] **Password recovery.** Run `docker exec <container> /requestarr reset-admin-password` privately; verify it prints one password, the old password and old sessions stop working, and the new password forces a change. With no local administrator it fails clearly and creates nothing.
- [ ] **Environment conflicts.** Add `REQUESTARR_OIDC_ISSUER`, `REQUESTARR_OIDC_CLIENT_ID` and `REQUESTARR_OIDC_CLIENT_SECRET` to the container and restart: Settings, Authentication shows **Set by environment**, read-only, and, with different saved values, **Saved settings are ignored** listing what differs, with **Discard saved settings**. Remove the secret variable only and restart: the card says the environment configuration is incomplete and names the missing variable, and OIDC stays off while `/login` still works. Remove all three and restart: the saved settings are editable again.
- [ ] **Upgrade keeps the environment OIDC.** On an instance upgraded from an earlier release with OIDC in its environment and existing users, confirm OIDC still works, no local administrator was created, and **Create local administrator** in Settings, Authentication shows the password once and forces a change.
- [ ] **Library over a private `http://` address.** Add a Plex, Jellyfin or similar Source by LAN address (and, on a user-defined network, by container name) over `http://`, save its address and credential in Settings, Integrations (a library applies at once, with no restart), and watch a sync complete with items showing as available. An `http://` address on a public DNS name is rejected with a message that says why.
- [ ] **IGDB client ID.** In Settings, Integrations, Metadata, enter the IGDB client ID beside the IGDB secret and save it; Games appears as a search type at once (with the secret set), and clearing the secret removes it again. With `REQUESTARR_IGDB_CLIENT_ID` set, the field is read-only and says the environment wins. On an installation upgraded from the draft wizard that had saved a client ID, the field shows the old value without any action.
- [ ] **Master key separation.** Restore a database dump into a fresh container with a different master key: the container starts, and saved credentials report "stored secret unreadable" instead of crashing.
