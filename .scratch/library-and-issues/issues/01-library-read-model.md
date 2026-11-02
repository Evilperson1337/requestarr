# Library read model and API

Status: ready-for-agent
Blocked by:

Browse Library items by media type with search, Source filter and newest-added order; songs only under their album.

- `GET /library/items?type=&q=&source=&cursor=`, cursor pagination like `/requests`.
- `GET /library/items/{id}`: title, year, Source, added date, cover URL, Source link, and the open Issue count.
- Albums list their tracks. Items in more than one library are one entry per Source.
- OpenAPI, role matrix row not needed (GET). Integration test over `inventory_items`.

Acceptance: the Library tabs can be built entirely from these two endpoints.
