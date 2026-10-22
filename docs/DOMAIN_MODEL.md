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

`(request_id, user_id)` unique. The requester gets source=`requester` automatically. Other users use source=`manual` from **Notify Me**. This captures notification interest without turning Requestarr into a voting system. Any subscriber, the creator included, can leave (D016): the Request stays open while others remain and becomes `cancelled` (with an audit event) when the last subscriber of an open Request leaves and confirms the cancellation. Admins can cancel an open Request outright.

## Inventory item

Normalized representation of an item found in an authoritative source. Each source retains its own source item ID and external URL plus normalized external IDs.

## Artwork

- `poster`: movie, TV, game;
- `square`: album, song.

Do not force all artwork into a single aspect ratio.
