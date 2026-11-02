# Issues UI

Status: resolved
Blocked by: 02, 04

Report dialog (kind list per media type, description, part picker for series and albums), Issues page (list, filters, detail with comments and admin actions), admin-only open-count badge in the sidebar.

Acceptance: Playwright tests per role; phone layout; axe clean.

## Answer

Built: report form inside the item dialog (kinds come from the API per media type; series can name a season and episode; Other needs words; rate limit explained; joining an open Issue says so), Issues page (status filter, list, detail with timeline, admin reporters, comments, Resolve / Close (won't fix) / Reopen, reporters can stop following), admin-only open-count badge. Not built: an album track picker for a report (Library items carry no tracklist yet), so a music report describes the track in its details.

## Follow-up (done)

Album tracks are now reported. Plex tracks carry `parentRatingKey`, `index` and `parentIndex`; Navidrome songs come inside their album. Both adapters record a track's album, disc and position (`inventory_items.parent_source_item_id`, `disc`, `position`, migration 29). `GET /library/items/{id}` returns an album's `tracks` (the list stays light), and the report form lets you tick the tracks affected. Verified against the live Plex library (17 tracks on one album; the Issue row reads "Audio · 1 track: Black, White, Tan").
