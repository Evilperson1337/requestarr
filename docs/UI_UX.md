# UI / UX Contract

The images under `references/mockups/final/` capture approved direction, not exact implementation dimensions.

## Home

V1 focuses on a large `Request Something` URL field, supported-provider hints, recent shared requests, recently available items, and an admin-only `Needs Review` summary. Do not fabricate recommendation/discovery shelves before native discovery exists.

A recognized pasted URL may resolve automatically. Resolution errors remain inline; successful resolution opens the modal.

## Universal request modal

The modal is the core interaction surface and must be reusable later from URL entry, search, discovery, or similar-item links.

States:
1. **Not requested/not available** -> request controls.
2. **Already requested** -> request metadata + `Notify Me` / `Notifications On` + View Request.
3. **Available** -> source availability and `Open in Plex/Jellyfin/Navidrome/RomM` when possible.
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

## Get started checklist

After local administrator sign-in and the forced password change, superusers can open a short checklist (D038) computed from live state. Required items (a library connected and synced, a metadata provider) and optional ones (notifications, the default role for new people) are listed apart, each with a text status and a link to the Settings section that applies it. The page has no fields of its own and can be dismissed for the instance. The earlier seven-step draft wizard with a Review step was retired.
