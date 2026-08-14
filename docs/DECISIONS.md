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
