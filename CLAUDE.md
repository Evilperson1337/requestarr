# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Requestarr is a self-hosted shared request/availability/notification portal for movies, TV, music, and games. It is currently a **design-to-implementation scaffold**: most `internal/` packages are single-file stubs (e.g. `internal/inventory/plex`, `internal/providers/metadata/tmdb`, `internal/auth/oidc`) awaiting real implementation. Read `docs/PROJECT_VISION.md` and `docs/V1_SCOPE.md` before writing feature code so you know what's actually in scope.

**Read `AGENTS.md` first.** It contains binding product-boundary and engineering rules for any AI agent working in this repo (no acquisition/downloader behavior, OIDC-only auth, one shared request per item, domain logic isolated from HTTP/DB/provider clients, etc.). `docs/DECISIONS.md` (D001–D012) records accepted architecture/product decisions — do not silently reverse them; if one seems wrong, document the conflict in `docs/DECISIONS.md` instead of quietly diverging.

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

The Go module targets 1.23+ syntax for compatibility; the dev container itself runs Go 1.24. The web scaffold has no third-party runtime deps beyond React/Vite — deliberately, so the architecture can be reviewed before further libraries are added.

## Architecture

Modular monolith: one Go process serves the HTTP API, (eventually) serves the production UI, runs background jobs, and talks to a single PostgreSQL database. Do not split into microservices/workers prematurely. External systems (metadata providers, media libraries, notification gateway) sit behind narrow interfaces in `internal/`, never called directly from HTTP handlers or domain logic.

Module boundaries (`internal/`):
- `auth/oidc` — OIDC discovery/login/callback/session/role mapping.
- `providers/metadata` (+ `tmdb`, `musicbrainz`, `igdb`) — turns a pasted URL into a canonical catalog item. Answers "what is this?".
- `domain` — core types (`Request`, `RequestStatus`, `PlatformSelection`, catalog/media types) and pure business rules (e.g. `ValidatePlatformSelection`). Keep this free of HTTP/DB/provider imports.
- `store` — repository interfaces close to domain use-cases, not a generic ORM; PostgreSQL is the only V1 target; SQL is explicit.
- `inventory` (+ `plex`, `jellyfin`, `navidrome`, `romm`) — normalized snapshots of what each authoritative library actually has. Answers "do we have it?".
- `reconcile` — matches inventory changes against active requests and drives status transitions.
- `notifications` (+ `apprise`) — fans out domain events to in-app notifications and the Apprise API.
- `jobs` — persistent background task/sync coordination.
- `setup` — first-run wizard draft/validation/finalization state.
- `httpapi` — transport only; no domain policy should live in handlers (see `internal/httpapi/router.go` for the current minimal scaffold: `/healthz`, `/api/v1/version`, `/api/v1/setup/status`).
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

`web/` is an independent React 19 + TypeScript + Vite scaffold (`web/src/App.tsx`, `web/src/pages`, `web/src/components`). It is not yet wired into the Go binary/release build. `references/mockups/final/` holds the authoritative UI decision mockups — follow them for behavior/decisions, but accessibility and responsive layout take precedence over pixel-perfect matching (per `AGENTS.md`).

## Working with reference material

`references/source/manifest.yaml` lists upstream projects that informed Requestarr's design. `./scripts/fetch-references.sh` (or `.ps1` on Windows) populates gitignored local checkouts — these are excluded from the repo on purpose. Do not copy implementation code from fetched checkouts into Requestarr; they're architectural/UX references with independent (some GPL/AGPL) licenses, and Requestarr's own license is still undecided (`LICENSE.md`).
