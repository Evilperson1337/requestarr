# Domain Model

## Catalog item

A canonical metadata-provider object. V1 uniqueness is `(provider, provider_id, media_type)`. The planned audiobook specialization uses Audible ASIN plus normalized storefront region as its provider identity; it is not implemented in the current model or schema.

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
- planned provider-native audiobook artwork: preserve the supplied aspect ratio; do not treat artwork as matching evidence.

Do not force all artwork into a single aspect ratio.

## Planned audiobook manifestation evidence

Before implementation, audiobook catalog and inventory records must be able to retain narrator, publisher, language, runtime, release date, abridged state, presentation type, and alternate stable identifiers when providers expose them. Exact/high-confidence reconciliation needs method, confidence, and bounded evidence; possible matches require review. This preserves a migration path to a V2 canonical work that can relate ebook and audiobook manifestations without imposing that hierarchy on V1.
