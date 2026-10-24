# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Requestarr is a self-hosted shared request/availability/notification portal for movies, TV, music, and games. It is a **working prototype, not yet V1-complete**: OIDC login, the TMDB/MusicBrainz/IGDB resolvers, shared requests with subscriptions, Jellyfin, Plex and RomM sync, in-app notifications and a global Apprise outbox are implemented. Navidrome is a README-only stub, and there is no real setup wizard. `docs/IMPLEMENTATION_PLAN.md` and `docs/research/PROJECT_STATUS.md` are the status sources; the V1 build plan is in `.scratch/v1-completion/`. Read `docs/PROJECT_VISION.md` and `docs/V1_SCOPE.md` before writing feature code so you know what's actually in scope. Domain vocabulary is in `GLOSSARY.md`.

**Read `AGENTS.md` first.** It contains binding product-boundary and engineering rules for any AI agent working in this repo (no acquisition/downloader behavior, OIDC-only auth, one shared request per item, domain logic isolated from HTTP/DB/provider clients, etc.). `docs/DECISIONS.md` (D001–D021) records accepted architecture/product decisions — do not silently reverse them; if one seems wrong, document the conflict in `docs/DECISIONS.md` instead of quietly diverging.

The `prompts/` directory contains a numbered sequence of implementation-phase prompts (`00_MASTER_CONTEXT.md` through `10_HARDEN_RELEASE.md`) intended to be run in order for AI-driven implementation work — check `prompts/README.md` and the current state of the repo to see which phase applies.

## Commands

```bash
make fmt            # gofmt the Go tree
make test           # go test ./...
make vet            # go vet ./...
make build          # CGO_ENABLED=0 build to bin/requestarr
make run            # go run ./cmd/requestarr
make web-install    # cd web && npm install
make web-dev        # cd web && npm run dev (Vite)
make web-build      # cd web && npm run build (tsc -b && vite build)
make web-check      # cd web && npm run check (tsc -b --pretty false, no lint step yet)
make dev-up         # docker compose -f compose.dev.yaml up -d (Postgres, Keycloak, Apprise API)
make dev-down
make snapshot       # goreleaser release --snapshot --clean
make release-check  # goreleaser check
```

Single Go test: `go test ./internal/domain/... -run TestName -v`.

Before opening a PR, run `scripts/check.sh` (fmt, test, vet, and web-check if `web/node_modules` exists). This mirrors what `AGENTS.md` calls "required checks before completing a change" — run all four (`make fmt test vet web-check`) after any change, not just Go changes.

The Go module and development container target Go 1.27. The web app has no third-party runtime deps beyond React/Vite — deliberately, so the architecture can be reviewed before further libraries are added.

## Architecture

Modular monolith: one Go process serves the HTTP API, serves the embedded production UI, runs background jobs, and talks to a single PostgreSQL database. Do not split into microservices/workers prematurely. External systems (metadata providers, media libraries, notification gateway) sit behind narrow interfaces in `internal/`, never called directly from HTTP handlers or domain logic.

Module boundaries (`internal/`):

- `auth/oidc` — OIDC discovery/login/callback/session/role mapping.
- `providers/metadata` (+ `tmdb`, `musicbrainz`, `igdb`) — turns a pasted URL into a canonical catalog item. Answers "what is this?".
- `domain` — core types (`Request`, `RequestStatus`, `PlatformSelection`, catalog/media types) and pure business rules (e.g. `ValidatePlatformSelection`). Keep this free of HTTP/DB/provider imports.
- `store` — repository interfaces close to domain use-cases, not a generic ORM; PostgreSQL is the only V1 target; SQL is explicit.
- `inventory` (+ `plex`, `jellyfin`, `navidrome`, `romm`; `plex`, `jellyfin` and `romm` are implemented, `navidrome` is README-only; every adapter implements `inventory.Source` and runs the `inventorytest` contract suite) — normalized snapshots of what each authoritative library actually has. Answers "do we have it?".
- `notifications` (+ `apprise`) — fans out domain events to in-app notifications and the Apprise API.
- `jobs` — the D018 run recorder and no-overlap guard: `Runner` records each run (start, finish, outcome, safe error) in `job_runs`, a per-Source lease in PostgreSQL stops overlap across restarts, and `Schedule` retries failures on the next tick. Jellyfin sync runs through it (`internal/app`), on the schedule and via the admin "Sync now" action (`POST /admin/sources/{source}/sync`), which share one guard.
- `setup` — first-run wizard draft/validation/finalization state.
- `httpapi` — transport only; no domain policy should live in handlers (see `internal/httpapi/router.go` for the routes, and `api/openapi.yaml` for the contract).
- `app` — wires config + logger + HTTP server lifecycle (`internal/app/app.go`).

Data flow for the core loop (see README's architecture diagram and `docs/ARCHITECTURE.md`):

```
metadata provider URL -> resolver -> canonical catalog item -> request
request + subscriptions live in Postgres
library sync -> normalized inventory -> InventoryChanged event -> targeted reconciliation
      -> status transition to `available` -> RequestAvailable event -> notifications (in-app + Apprise)
```

Key invariants worth knowing before touching this flow:

- A source that fails to sync must never generate removals; true "no longer available" detection requires a successful authoritative enumeration, not an error path.
- Reconciliation should be targeted (affected items), not a full N×M sweep, when it's possible to scope it.
- Request status and availability are conceptually separate even though V1 UI/API collapses `available` into the status enum (`internal/domain/request.go`).
- Game requests use OR-semantics platform matching: `Any` is mutually exclusive with explicit `PlatformKeys`; `ValidatePlatformSelection` in `internal/domain/request.go` encodes this — reuse it rather than re-deriving the rule.
- Subscriptions are `(request_id, user_id)` unique; the requester gets `source=requester` automatically, others get `source=manual` via "Notify Me" — this is not a voting system, do not add vote counts.

Database migrations under `db/migrations/` are append-only once merged — never edit an applied migration; add a new one.

`api/openapi.yaml` is the HTTP contract; treat it as a source of truth alongside the domain types, not as documentation generated after the fact.

## Frontend

`web/` is a React 19 + TypeScript + Vite app (`web/src/App.tsx`, `web/src/pages`, `web/src/components`). `make build` builds it and embeds it in the Go binary via `internal/webui`; the Dockerfile does the same. GoReleaser builds them in its `before` hooks; `internal/webui/dist` is build output and is not committed (see `docs/RELEASES.md`). `references/mockups/final/` holds the authoritative UI decision mockups — follow them for behavior/decisions, but accessibility and responsive layout take precedence over pixel-perfect matching (per `AGENTS.md`).

## Working with reference material

`references/source/manifest.yaml` lists upstream projects that informed Requestarr's design. `./scripts/fetch-references.sh` (or `.ps1` on Windows) populates gitignored local checkouts — these are excluded from the repo on purpose. Do not copy implementation code from fetched checkouts into Requestarr; they're architectural/UX references with independent (some GPL/AGPL) licenses, and Requestarr's own license is AGPL-3.0 (D013); `LICENSE.md` carries the AGPL-3.0 text.

## Agent skills

### Issue tracker

Issues and specs are local markdown files under `.scratch/<feature>/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Use the default five-role vocabulary, recorded as a `Status:` line in each issue file. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: read the root `GLOSSARY.md` and relevant architecture/product decisions in `docs/DECISIONS.md` (D001+), not `docs/adr/`. See `docs/agents/domain.md`.
