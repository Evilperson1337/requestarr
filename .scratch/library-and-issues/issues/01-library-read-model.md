# Library read model and API

Status: resolved
Blocked by:

Browse Library items by media type with search, Source filter and newest-added order; songs only under their album.

- `GET /library/items?type=&q=&source=&cursor=`, cursor pagination like `/requests`.
- `GET /library/items/{id}`: title, year, Source, added date, cover URL, Source link, and the open Issue count.
- Albums list their tracks. Items in more than one library are one entry per Source.
- OpenAPI, role matrix row not needed (GET). Integration test over `inventory_items`.

Acceptance: the Library tabs can be built entirely from these two endpoints.

## Answer

Built: `GET /library/items` (type, q, source, limit, cursor) and `GET /library/items/{id}`; keyset paging; one entry per title per Source; only the latest snapshot; Issue counts included. Album tracks are not listed: Plex tracks carry no parent-album reference yet (needs `parentRatingKey`), so the Library page shows albums only and the Issue part for an album comes from the catalog tracklist as already designed.
