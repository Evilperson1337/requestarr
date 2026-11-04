# Architecture

## Architectural style

Requestarr should begin as a modular monolith. A single Go process exposes the HTTP API, serves the production UI, runs background jobs, and writes PostgreSQL. External services remain adapters behind narrow interfaces. Do not prematurely split workers/microservices.

## Modules

Packages under `internal/`:

- `auth/oidc` — OIDC discovery/login/callback/session/role mapping.
- `providers/metadata` (+ `tmdb`, `musicbrainz`, `igdb`) — turns a pasted URL into a canonical catalog item, and (from V1.x, D032) finds candidate items by name as refs this resolver accepts. Answers "what is this?".
- `sources` — the registry of Source kinds (slug, ID, media types, credential fields); plain data, imports only `domain`.
- `secrets` — stored integration secrets (D024): registry, sealing, resolver. `opstatus` — the operational status overview.
- `domain` — core types (`Request`, `RequestStatus`, `PlatformSelection`, catalog/media types) and pure business rules (e.g. `ValidatePlatformSelection`). Keep this free of HTTP/DB/provider imports.
- `store` — repository interfaces close to domain use-cases, not a generic ORM; PostgreSQL is the only V1 target; SQL is explicit. `store/snapshottest` is the snapshot-persistence contract both the store and an in-memory adapter run.
- `inventory` (+ `plex`, `jellyfin`, `navidrome`, `romm`; all four are implemented; every adapter implements `inventory.Source` and runs the `inventorytest` contract suite) — normalized snapshots of what each authoritative library actually has. Answers "do we have it?".
- `notifications` (+ `apprise`) — fans out domain events to in-app notifications and the Apprise API.
- `jobs` — the D018 run recorder and no-overlap guard: `Runner` records each run (start, finish, outcome, safe error) in `job_runs`, a per-Source lease in PostgreSQL stops overlap across restarts, and `Schedule` retries failures on the next tick. Every Source's sync runs through it (`internal/app`), on the schedule and via the admin "Sync now" action (`POST /admin/sources/{source}/sync`), which share one guard.
- `setup` — first-run wizard draft/validation/finalization state.
- `httpapi` — transport only; no domain policy should live in handlers. `NewRouter(Deps)` takes every dependency explicitly; `access.go` is the route table holding each route's access and CSRF policy; `api/openapi.yaml` is the contract, and a test keeps the two in step.
- `app` — startup: `resolveBoot` (saved IGDB ID, secrets, validation), `Build` (derives the runtime from the resolved config), and `Run` (starts the server and jobs).
- `devlogs` — the development-only connection-check recorder.

Data flow for the core loop:

```
metadata provider URL -> resolver -> canonical catalog item -> request
request + subscriptions live in Postgres
library sync -> normalized inventory -> InventoryChanged event -> targeted reconciliation
      -> status transition to `available` -> RequestAvailable event -> notifications (in-app + Apprise)
```

## Fast-path principle

Routine UI requests should primarily read PostgreSQL. External API calls belong in URL resolution, name search (D032; an explicit user action like resolving, never page rendering), explicit connection tests, and background refresh/sync jobs. Known stale data is generally preferable to blocking the UI.

## Sync and reconciliation

```text
Source sync -> normalized changes -> inventory transaction -> InventoryChanged event
                                                |
                                                v
                                    targeted reconciliation
                                                |
                          status transition if satisfied
                                                |
                              RequestAvailable event
                                                |
                        subscribed users -> notifications
```

A source that is unreachable must fail its sync without generating removals. True removal detection occurs only after a successful authoritative enumeration/delta cycle.

## Shared queue pagination

Request listing uses local state only. Query validation lives in the requests
use-case package; HTTP only parses parameters and maps errors. A first-page
query captures at most 10,000 matching request IDs in deterministic order. When
more than one page is needed, PostgreSQL retains that ordered ID array for
15 minutes, with at most five snapshots per user. Snapshot keys sign opaque
cursors and persist alongside the ID array; no process-local mutable state or
long-lived database transaction is needed.

Pages read current request details, never reapply mutable filters to snapshot
members. This avoids skips/repeats when status sort, title search, or My Requests
membership changes. New/moved requests appear when a view is refreshed without
a cursor. Expiry and capacity errors are explicit. Snapshot creation serializes
per-user retention and performs bounded opportunistic expiry cleanup. See the
[request queue API guide](REQUESTS_API.md) for the public semantics and limits.

## Matching hierarchy

Prefer stable external IDs over title matching.

- Movie/TV: TMDB first; retain IMDb/TVDB identifiers when available.
- Music album: MusicBrainz release-group.
- Music song: MusicBrainz recording.
- Games: IGDB canonical game + platform matching.
- Fallback title/year matching must record confidence/reason and be reviewable.

When no stable identifier matches, a complete inventory sync records reviewable
title/year candidates separately from availability evidence. Candidate lookup
uses an index on media type and normalized title: lower-case, trim, and collapse
punctuation/whitespace into word separators. Titles must match exactly after
normalization, media types must agree, and years must agree when both exist.
A missing year is explicitly lower confidence (`title_only`) with a reason.

Admins review candidates under **Settings → Suggested matches**. Acceptance
records actor/time and normal source/platform evidence in the same transaction
as availability, request events, and notifications. An accepted match is local
to that catalog/library-item pair, not a global provider alias. Later complete
syncs revalidate it and maintain evidence through the ordinary reconciliation
path. If the item disappears or stops matching, ordinary absence safeguards
apply. Exact identifier matches always take precedence.

Dismissal is terminal for the pair and never changes availability. Both review
decisions are idempotent and audited; a conflicting decision returns a conflict.
Only current candidates from the last successful complete snapshot can be
accepted. Failed syncs preserve the last-known view; a successful snapshot that
omits the item makes the candidate stale, including guarded empty snapshots.

## Game fulfillment

For an IGDB game request:
- `Any = true` means any RomM platform instance of the canonical game satisfies it.
- Explicit platform keys are a set of acceptable alternatives.
- Presence of one matching selected platform satisfies the request.
- Evidence is tracked per platform (D021); a platform a Source cannot map to IGDB satisfies Any but never an explicit selection.
- `Any` and explicit selections are mutually exclusive.

## Adding a Library item field

A fact about a Library item travels adapter, snapshot, database, API, dialog. Touch each stop; the first two checks below catch a missed one.

1. `internal/domain/library.go`: the field on `LibraryItem` (or `LibraryDetails`), with its JSON tag and any bounds in `Clean`.
2. `internal/inventory/source.go`: the field on `inventory.Item`.
3. Each adapter under `internal/inventory/<kind>/client.go` that knows the fact, with a test in its `client_test.go`. Check the library's field names against its upstream source (`references/source/manifest.yaml`) or a real response first.
4. `db/migrations/`: a new migration (never edit an applied one) and the upsert in `internal/store/postgres/inventory.go`. A field missing from the upsert is silently dropped.
5. `internal/store/postgres/library_browse.go`: the select. The list stays small; single-item reads carry the heavy fields.
6. `api/openapi.yaml`: the schema. `TestOpenAPILibrarySchemasMatchTheGoTypes` fails if it disagrees with the Go struct.
7. `web/src/api.ts` and `web/src/components/LibraryItemDialog.tsx`, with a Playwright case in `web/tests/library-issues.spec.ts`.

`TestLibraryItemDetailsRoundTripIntegration` runs steps 4 and 5 against PostgreSQL; extend it with the new field.
