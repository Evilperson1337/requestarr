# UI / UX Contract

The images under `references/mockups/final/` capture approved direction, not exact implementation dimensions.

## Home

V1 focuses on a large `Request Something` URL field, supported-provider hints (TMDB, MusicBrainz, IGDB, Audible/Audnexus), recent shared requests, recently available items, and an admin-only `Needs Review` summary. Do not fabricate recommendation/discovery shelves before native discovery exists.

A recognized pasted URL may resolve automatically. Resolution errors remain inline; successful resolution opens the modal.

## Universal request modal

The modal is the core interaction surface and must be reusable later from URL entry, search, discovery, or similar-item links.

States:
1. **Not requested/not available** -> request controls.
2. **Already requested** -> request metadata + `Notify Me` / `Notifications On` + View Request.
3. **Available** -> source availability and `Open in Plex/Jellyfin/Navidrome/RomM` when possible.
4. **Partially available** -> show child/unit state where applicable.

Audiobook request presentation should show author, narrator, series/position when available, runtime, Audible region/ASIN provenance, and source-native artwork. If an identical request exists, show its shared status and `Notify Me` rather than permitting a duplicate. If Audiobookshelf already contains the item, show `Available` and an Audiobookshelf deep link when possible.

Game request controls dynamically render IGDB-supported platforms:

```text
[ ] Any platform
[ ] PC (Windows)
[ ] PlayStation 5
[ ] PlayStation 4
[ ] Xbox Series X|S
...
```

`Any` is mutually exclusive. Explicit platforms are multi-select OR conditions.

## Requests page

Shared by all authenticated users. Supports:
- text search;
- status chips;
- type filter;
- requester filter;
- `My Requests` quick filter;
- sort;
- requester attribution;
- status; platform/details; actions.

Do not add a separate private My Requests route as a primary navigation destination.

## Artwork

Rows/cards allocate a consistent bounding area but preserve source ratio:
- poster: 2:3-ish;
- square: 1:1;
- audiobook: preserve provider-native cover shape (commonly portrait or square).

Mixed-media lists should look intentionally heterogeneous rather than cropping square album art into posters.

## Settings

Admin-only nested settings navigation:
- General
- Authentication
- Users & Access
- Providers
- Libraries
- Requests
- Notifications
- System Status / Jobs / Logs / About

Library cards show health, last successful sync, next sync, enabled authoritative media types, item counts, Test Connection, Sync Now, Edit.

## Setup wizard

Persistent left-side progress list with clear current/completed steps. External configuration steps expose validation/test actions before continuing. Final Review summarizes every consequential setting before setup is committed and bootstrap access is invalidated.

## Post-V1 discovery direction

Seerr is the primary cross-media discovery/status UX reference; Libreseerr demonstrates that the same card/detail/request language can extend to book/audiobook domains. Requestarr should eventually feel like browsing a catalog that understands local availability, not like filling out a ticket form.

Discovery rails should be media-aware so artwork remains intentional (poster/cover rails for movies/TV/games/audiobooks where appropriate; square rails for music), while universal search may aggregate provider results progressively. Cards should emphasize only the user-facing state: `Available`, `Requested`, `Approved`, `Request`, and `Notify Me` for an existing request. Operational match/source details belong in item/admin surfaces.
