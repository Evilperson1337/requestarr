# Domain Model

## Catalog item

A canonical metadata-provider object. V1 uniqueness is `(provider, provider_id, media_type)`.

## Request

A shared desired-state record. V1 intentionally has a unique `catalog_item_id`; the first user creates the request and later users subscribe rather than create duplicates.

Status:
- `requested` — submitted and awaiting admin decision when approval is required;
- `approved` — accepted but not yet observed in an authoritative library;
- `denied` — rejected;
- `available` — reconciliation found a satisfying inventory item;
- `cancelled` — withdrawn/administratively closed.

Availability should be modeled separately in services/read models even though `available` is exposed as a convenience status in V1.

## Request subscription

`(request_id, user_id)` unique. The requester gets source=`requester` automatically. Other users use source=`manual` from **Notify Me**. This captures notification interest without turning Requestarr into a voting system.

## Inventory item

Normalized representation of an item found in an authoritative source. Each source retains its own source item ID and external URL plus normalized external IDs.

## Artwork

- `poster`: movie, TV, game;
- `square`: album, song.

Do not force all artwork into a single aspect ratio.

## Availability match evidence

`request_availability_matches` records why Requestarr believes an inventory item satisfies a request. It captures source/inventory identity, a cross-media match method, confidence (`exact`, `high`, `possible`), bounded evidence, and active timestamps. Only exact/high matches may automatically transition a request to `available`; possible matches require review.

## Audiobook manifestation identity

Audiobook V1 is edition-aware. Store ASIN + Audible region when available plus narrator(s), publisher, release date, language, runtime, abridged state, edition/presentation type, series context, and alternate stable IDs. These attributes are sufficient to distinguish materially different narrations/productions while leaving room for ebook V2 to introduce a broader canonical work relation.
