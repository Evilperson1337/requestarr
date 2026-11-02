# Audiobookshelf inventory adapter

Status: ready-for-agent
Blocked by: 01, 03

New `internal/inventory/audiobookshelf` implementing `inventory.Source`, passing `inventorytest`.

- `GET /api/libraries`, keep `mediaType == book`; page `GET /api/libraries/:id/items?minified=1&limit=&page=` with a response-size cap. Do not use `limit=0` on large libraries.
- Emit `Item{MediaType: audiobook, ExternalIDs{"asin"}, Title, Detail: author + narrator, AddedAt, ExternalURL}` for items with `numTracks > 0`. Skip ebook-only and `isMissing` items. Items without an ASIN are emitted without the ID so the candidate review can see them.
- Any failed or truncated page, 401 or timeout returns an error and no snapshot (D015). An empty enumeration is never absence.
- Read-only: a contract test fails if the client issues any non-GET request.
- Fixtures for ABS pages: normal, missing ASIN, `isMissing`, ebook-only, expired key, truncated page. The in-repo OpenAPI is stale (research §1), so fixtures come from a real server response.

Acceptance: a successful sync marks a matching ASIN Request Available; a failed sync changes nothing.
