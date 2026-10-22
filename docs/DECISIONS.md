# Architecture / Product Decision Log

These are accepted decisions unless explicitly superseded by a dated entry.

## D001 — No acquisition layer
Requestarr only manages requests, availability, and notifications. No *arr/downloader integration in V1.

## D002 — Link-first V1
Users paste metadata provider links instead of searching an internal/global catalog. Native discovery can be added later without changing the request modal/domain path.

## D003 — Shared single request
One canonical request exists per item. The requester's identity is attribution, not privacy/ownership. Requests are visible to every authenticated user.

## D004 — Notify Me subscriptions
The requester is subscribed automatically. Other users may subscribe to request state changes without creating a duplicate request or vote.

## D005 — OIDC required
OIDC is mandatory for normal V1 operation. Keycloak is the primary tested provider. First-run bootstrap uses a temporary setup mechanism only.

## D006 — Apprise integration
In-app notifications are native; external notifications use Apprise API instead of implementing many destination-specific adapters.

External Apprise delivery uses one operator-configured global route (environment URL + configuration key), enqueued once per event; native inbox delivery remains subscriber-scoped. Migration 000010 is retained unchanged and its user-level Apprise preferences are deprecated. Migration 000011 introduces the dedicated global outbox.

## D007 — Library authority
Plex/Jellyfin determine movie/TV availability, Plex/Jellyfin/Navidrome determine music availability, and RomM determines game availability. A source is authoritative only for configured media types.

## D008 — Local state + background sync
UI state is primarily served from local PostgreSQL. External slowness/outages should not block normal pages. Source failures retain last-known inventory.

## D009 — Game platforms are dynamic OR criteria
IGDB defines which platforms to display. UI shows Any first and then provider platforms as multi-select checkboxes. Any selected platform may satisfy the request.

## D010 — Native artwork proportions
Poster media remains ~2:3; album/song artwork remains square. UI normalizes layout containers, not the art itself.

## D011 — Shared Requests navigation
Primary navigation says `Requests`, not `My Requests`. My Requests is a filter/button on the shared page.

## D012 — Setup wizard is V1
First run guides General -> Authentication -> Metadata Providers -> Libraries -> Notifications -> Request Policy -> Review -> Finish, with external connection validation and resumability.

## Migration 000007 — Correct in-place before release
Migration 000001 already creates and seeds the singleton `setup_state` row. Migration 000007 extends that table with admin-bootstrap fields and must not create a second table or seed row. Editing 000007 in place is safe only because it is newly authored, unmerged/unreleased, and has not been applied in production; once shipped or applied, migrations are append-only and corrections require a new migration.

## D013 — License is AGPL-3.0
Requestarr is a network service; AGPL-3.0 keeps hosted forks open. The Go module path is `github.com/evilperson1337/requestarr`, matching the real remote. The default branch is `main`.

## D014 — Request status `available` is projected from Availability
When a Source sync makes a catalog item available, open Requests for it move to `available` in the same transaction, with a version bump and an audit event, and the `RequestAvailable` event is emitted from that path. The unused `internal/reconcile` package was removed; availability matching lives in `internal/store/postgres/inventory.go` until the adapter interface (ticket 11) is designed.

## D015 — Availability is "present in at least one Source"
An item becomes missing only after every covering Source completes a successful sync without it. Failed syncs never produce removals. Absence is tracked per (Source, item) in `source_absence_state`: a Source drops its own evidence after two complete syncs a day apart without the item, and the item turns missing only when no enabled Source covering its media type still has evidence. Every adapter implements `inventory.Source` and must pass `internal/inventory/inventorytest`.

## D016 — Cancellation of shared Requests
A Requester cancelling leaves the Request open if other Subscribers remain; it becomes `cancelled` only when the last Subscriber leaves or an admin cancels it. Admins can set `viewer` as well as `requester`/`admin`, and the last admin cannot be demoted. Leaving is idempotent by state and takes no operation key; a leave that would cancel the Request must be confirmed explicitly (`confirmCancel=true`), so the last-subscriber warning is decided by the server, not a stale page. Admin cancel uses the existing transition `expectedVersion`/`operationId`.

## D017 — Secrets stay environment-only in V1
The setup wizard persists non-secret settings (URLs, library choices) and the names of environment variables holding secrets. Encrypted secrets in PostgreSQL are deferred. The wizard must state clearly what still needs an env edit and restart.

## D018 — V1 job runner is Postgres-backed and minimal
A job-run table with status, timestamps and errors, a no-overlap guard that survives restarts, an admin "sync now" action and a status endpoint. No queue library or worker process; retries use the next scheduled tick. Lease and run times come from the database clock, a job whose lease cannot be renewed is cancelled, and each Source keeps its newest 200 runs.

## D019 — V1 build order
Baseline hygiene, then the `available` flow, then the job runner, then the adapter interface with Plex, RomM, Jellyfin (onto the new interface) and Navidrome in that order, then the Requests page, then wizard/settings/ops status, then release hardening.
