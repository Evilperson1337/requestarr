# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Requestarr is a self-hosted shared request/availability/notification portal for movies, TV, music, and games. It is **V1-scope-complete against fixtures but not yet release-ready**: OIDC login, the TMDB/MusicBrainz/IGDB resolvers, shared requests with subscriptions, Jellyfin, Plex, RomM and Navidrome sync, in-app notifications, a global Apprise outbox, the built-in local administrator and live Get started checklist (D038; the draft setup wizard was retired), settings that apply at once (D035, D039) and an operational status view are implemented. `docs/research/PROJECT_STATUS.md` (with its explicit gap list) and `docs/IMPLEMENTATION_PLAN.md` are the status sources; the V1 build plan is in `.scratch/v1-completion/`. Read `docs/PROJECT_VISION.md` and `docs/V1_SCOPE.md` before writing feature code so you know what's actually in scope. Domain vocabulary is in `GLOSSARY.md`.

**Read `AGENTS.md` first.** It contains binding product-boundary and engineering rules for any AI agent working in this repo (no acquisition/downloader behavior, OIDC for people and one built-in local administrator (D040), one shared request per item, domain logic isolated from HTTP/DB/provider clients, etc.). `docs/DECISIONS.md` (D001+) records accepted architecture/product decisions — do not silently reverse them; if one seems wrong, document the conflict in `docs/DECISIONS.md` instead of quietly diverging.

The `prompts/` directory contains a numbered sequence of implementation-phase prompts (`00_MASTER_CONTEXT.md` through `10_HARDEN_RELEASE.md`) intended to be run in order for AI-driven implementation work — check `prompts/README.md` and the current state of the repo to see which phase applies.

## Commands

```bash
make bootstrap      # fresh clone or worktree: npm ci + build the embedded UI (Go tests need it)
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

To cut a release (bump the pinned tag, then run `scripts/publish-release.sh <tag>`, which is non-interactive), follow "Cutting a release" in `docs/RELEASES.md`.

To look at a running instance (server log, status, sync runs, connections) without a superuser session, use a read-only diagnostics token with `scripts/agent/requestarr-diag.sh`; set it up and use it as `docs/DIAGNOSTICS_TOKEN.md` says (D043). Never print the token or pass it on a command line.

Single Go test: `go test ./internal/domain/... -run TestName -v`.

Before opening a PR, run `scripts/check.sh` (fmt, test, vet, PostgreSQL integration tests, and web typecheck plus Playwright if `web/node_modules` exists; `make bootstrap` prepares a fresh clone or worktree, and `REQUESTARR_SKIP_BROWSER_TESTS=1` skips the browser tests). This mirrors CI and what `AGENTS.md` calls "required checks before completing a change". Run it after any change, not just Go changes. A change under `internal/store/` or `db/migrations/` is unverified until `make test-integration` has passed (it picks a free Postgres port itself).

The Go module and development container target Go 1.27. The web app has no third-party runtime deps beyond React/Vite — deliberately, so the architecture can be reviewed before further libraries are added.

## Architecture

Modular monolith: one Go process serves the HTTP API, serves the embedded production UI, runs background jobs, and talks to a single PostgreSQL database. Do not split into microservices/workers prematurely. External systems (metadata providers, media libraries, notification gateway) sit behind narrow interfaces in `internal/`, never called directly from HTTP handlers or domain logic.

Module boundaries (`internal/`), the core data flow, sync and matching rules are in `docs/ARCHITECTURE.md`; read it before adding a module or moving logic between modules.

Key invariants worth knowing before touching this flow:

- A source that fails to sync must never generate removals; true "no longer available" detection requires a successful authoritative enumeration, not an error path.
- Reconciliation should be targeted (affected items), not a full N×M sweep, when it's possible to scope it.
- Request status and availability are conceptually separate even though V1 UI/API collapses `available` into the status enum (`internal/domain/request.go`).
- Game requests use OR-semantics platform matching: `Any` is mutually exclusive with explicit `PlatformKeys`; `ValidatePlatformSelection` in `internal/domain/request.go` encodes this — reuse it rather than re-deriving the rule.
- Subscriptions are `(request_id, user_id)` unique; the requester gets `source=requester` automatically, others get `source=manual` via "Notify Me" — this is not a voting system, do not add vote counts.

Before parsing a media library's or provider's JSON, check its field names against its upstream source or API docs rather than recalling them: `references/source/manifest.yaml` lists the checkouts `./scripts/fetch-references.sh` fetches (RomM today); for a project it lacks, add it to the manifest or record a real response in the adapter's `testdata`. To add a field to a Library item, follow "Adding a Library item field" in `docs/ARCHITECTURE.md`.

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
