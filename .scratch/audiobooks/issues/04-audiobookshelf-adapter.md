# Audiobookshelf inventory adapter

Status: resolved
Blocked by: 01, 03

New `internal/inventory/audiobookshelf` implementing `inventory.Source`, passing `inventorytest`.

- `GET /api/libraries`, keep `mediaType == book`; page `GET /api/libraries/:id/items?minified=1&limit=&page=` with a response-size cap. Do not use `limit=0` on large libraries.
- Emit `Item{MediaType: audiobook, ExternalIDs{"asin"}, Title, Detail: author + narrator, AddedAt, ExternalURL}` for items with `numTracks > 0`. Skip ebook-only and `isMissing` items. Items without an ASIN are emitted without the ID so the candidate review can see them.
- Any failed or truncated page, 401 or timeout returns an error and no snapshot (D015). An empty enumeration is never absence.
- Read-only: a contract test fails if the client issues any non-GET request.
- Fixtures for ABS pages: normal, missing ASIN, `isMissing`, ebook-only, expired key, truncated page. The in-repo OpenAPI is stale (research §1), so fixtures come from a real server response.

Acceptance: a successful sync marks a matching ASIN Request Available; a failed sync changes nothing.

## Answer

Built `internal/inventory/audiobookshelf` (passes `inventorytest`, extended to accept `audiobook`): book libraries only, paged `minified=1` items, strict page/total consistency, duplicate-id and malformed-item errors, no book library = error (not an empty snapshot), `isMissing`/`isInvalid`/`numTracks == 0` skipped, ASIN upper-cased into `ExternalIDs["audible"]` (the identifier kind is the catalog provider name, not `asin`), items without a usable ASIN still listed for the title review, Detail "Author · read by Narrator", item link `<origin>/item/<id>`. Tests assert every request is a GET and the key only travels in the Authorization header. The fixtures are hand-written from Audiobookshelf's server code, not captured from a real server.

Not done: covers (`ArtworkSource`) and the title/author fields on `inventory.Item` for candidate matching (ticket 05); `updatedAt` skipping.
