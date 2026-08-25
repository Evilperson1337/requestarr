# UI / UX Contract

The images under `references/mockups/final/` capture approved direction, not exact implementation dimensions.

## Home

V1 focuses on a large `Request Something` URL field, supported-provider hints, recent shared requests, recently available items, and an admin-only `Needs Review` summary. The future audiobook hint may name Audible only after that resolver is implemented. Do not fabricate recommendation/discovery shelves before native discovery exists.

A recognized pasted URL may resolve automatically. Resolution errors remain inline; successful resolution opens the modal.

## Universal request modal

The modal is the core interaction surface and must be reusable later from URL entry, search, discovery, or similar-item links.

States:
1. **Not requested/not available** -> request controls.
2. **Already requested** -> request metadata + `Notify Me` / `Notifications On` + View Request.
3. **Available** -> source availability and `Open in Plex/Jellyfin/Navidrome/RomM` when possible; a future audiobook implementation may add Audiobookshelf.
4. **Partially available** -> show child/unit state where applicable.

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
- square: 1:1.

Mixed-media lists should look intentionally heterogeneous rather than cropping square album art into posters.

## Planned audiobook presentation

No audiobook UI is implemented. When introduced, the resolved-item modal should identify the Audible storefront/edition and present narrator and other significant edition metadata without treating decorative similarity as availability. Preserve provider-native audiobook artwork proportions. A possible availability match belongs in an accessible admin review state, not an automatic `Available` badge; exact/high-confidence matching may be shown with concise provenance.

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
