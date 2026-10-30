# Request queue API

`GET /api/v1/requests` lists the shared queue for any authenticated user,
including viewers. It reads local PostgreSQL state only. The shared Requests
page exposes these controls; see the [page guide](REQUESTS_UI.md).

The complete contract is in [OpenAPI](../api/openapi.yaml).

## Query parameters

Each parameter may occur once. Unknown parameters, invalid percent encoding,
invalid UTF-8, control characters, and invalid values return the standard error
envelope. The encoded query string is bounded to 4096 bytes.

| Parameter | Meaning | Default |
| --- | --- | --- |
| `q` | Case-insensitive literal title substring; at most 200 Unicode characters; surrounding whitespace trimmed. `%`, `_`, quotes, and backslashes are literal, not pattern/SQL syntax. | All titles |
| `requestId` | Exact shared Request UUID used by View request. Combines with other filters and is part of cursor identity. | All Requests |
| `status` | One of `requested`, `approved`, `available`, `denied`, `cancelled`. This is request lifecycle, not catalog availability. | All statuses |
| `mediaType` | One of `movie`, `tv`, `album`, `song`, `game`. | All types |
| `mine` | Exactly `true` or `false`; filters by the current user's subscriptions, including Notify Me. Not a creator-only filter. | `false` |
| `sort` | `newest`, `oldest`, or `status`. | `newest` |
| `limit` | Integer from 1 through 100. | `100` |
| `cursor` | Opaque next-page cursor from the previous response. | First page |

Filters combine with AND. Newest and oldest use request creation time. Status
order is requested → approved → available → denied → cancelled, then newest.
All orders use ascending request ID as the final tie-breaker.

Example first page:

```http
GET /api/v1/requests?q=arrival&status=requested&mediaType=movie&mine=true&sort=oldest&limit=25
```

The response is `{ "items": [...], "nextCursor": "..." }`, with `nextCursor`
equal to `null` on the final page. Empty results use `items: []`. Existing
request fields, subscription state, availability, and `CatalogPlatforms` are
preserved. Responses are marked `Cache-Control: no-store`.

To fetch the next page, repeat the same filters, sort, and limit and append the
returned cursor, URL-encoded. Treat cursors as opaque; do not edit them. Query
identity is normalized, so explicit defaults and title-search casing/outer
whitespace do not create different views. Cursors belong to the authenticated
user and must not be reused by another user.

## Stability and refresh

When pagination is needed, the first page stores the matching request IDs and
their order in a short-lived PostgreSQL snapshot. It stores IDs only: every
page returns **current** titles, statuses, artwork, subscriptions, availability,
and platform details.

Changes to title, status, or subscription membership cannot make a request
repeat or disappear mid-traversal. Newly created or newly matching requests do
not enter that traversal. A row may therefore display a new status/title that
no longer matches the original filter: this is intentional. Physically deleted
requests, if removed outside the normal API, are omitted while cursor position
still advances. Refresh without a cursor to rebuild membership and order.

Snapshots and per-snapshot signing keys survive application restarts. Retrying
the same cursor visits the same positions until expiry; displayed details can
still change. No long-running database transaction is held between HTTP calls.

## Limits and errors

- Snapshots expire after **15 minutes**, measured by the database clock, not
  extended by page reads. Starting a sixth paginated view expires that user's
  oldest snapshot; at most five are retained per user. Single-page results do
  not consume a snapshot slot. Expired snapshots are cleaned opportunistically
  in bounded batches when new paginated views are created.
- At most **10,000 IDs** may match a view. Larger views return `422
  query_too_large`, never silently truncate. Narrow `q`, `status`, `mediaType`,
  or `mine` and retry.
- `400 invalid_query`: invalid, repeated, unsupported, or oversized parameters.
- `400 invalid_cursor`: malformed cursor, invalid signature for a known
  snapshot, different user, or changed query/limit. A cursor is at most 512 bytes.
- `410 cursor_expired`: snapshot expired, was evicted, or no longer exists.
  Restart without the cursor; keep the other filters if desired.
- `401 authentication_required`: no valid session.
- `500 requests_list_failed` / `503 requests_unavailable`: safe generic service
  errors. SQL, cursor signing keys, and raw errors are never returned.

Errors use `{ "error": { "code", "message", "requestId" } }`. Branch on `code`,
not message. Quote the request ID when reporting a failure.

## Movie and TV preview contracts

Authenticated metadata resolution includes **Movie** for movie details or **TV**
for whole-series details, plus **Preview** local state, alongside the existing
catalog item and platforms. The media-specific detail object is a
bounded typed metadata object, not the raw TMDB payload. It is persisted with the
catalog's existing JSON metadata, so no database migration is needed. Old sparse
catalog rows remain compatible. Optional credits use a 1.5-second sub-budget
inside a nine-second total TMDB lookup; a secondary failure retains core metadata.

TV details include first/last air dates, series status, per-episode runtime,
season/episode counts, and creators from TMDB's series-level created-by data.
Counts never add child-unit Request selection. Movie and TV numeric provider IDs
remain separate catalog namespaces, and a TV Request uses one canonical series ID.

The local catalog preview endpoint returns availability, the shared Request and
current user's subscription if one exists, usable library links when known
available, and role-appropriate canRequest/canSubscribe flags. The response is
no-store and performs no provider or library calls. The flags are advisory:
transactional writes still enforce roles, open/closed state, and idempotency.
The exact endpoint and field schemas are documented in OpenAPI.
