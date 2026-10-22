# Manual Testing Guide

Step-by-step verification of the foundation: config validation, PostgreSQL startup/migrations/readiness, and the frontend build. These steps do not cover the request, OIDC, Jellyfin sync or notification features, which are implemented but have no manual walkthrough here yet (see `docs/IMPLEMENTATION_PLAN.md` for current status). See `docs/TESTING.md` for the broader automated-testing strategy as more of the system is implemented.

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
