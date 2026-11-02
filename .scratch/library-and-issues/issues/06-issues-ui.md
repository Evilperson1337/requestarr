# Issues UI

Status: resolved
Blocked by: 02, 04

Report dialog (kind list per media type, description, part picker for series and albums), Issues page (list, filters, detail with comments and admin actions), admin-only open-count badge in the sidebar.

Acceptance: Playwright tests per role; phone layout; axe clean.

## Answer

Built: report form inside the item dialog (kinds come from the API per media type; series can name a season and episode; Other needs words; rate limit explained; joining an open Issue says so), Issues page (status filter, list, detail with timeline, admin reporters, comments, Resolve / Close (won't fix) / Reopen, reporters can stop following), admin-only open-count badge. Not built: an album track picker for a report (Library items carry no tracklist yet), so a music report describes the track in its details.
