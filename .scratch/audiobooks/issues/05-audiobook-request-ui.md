# Request audiobook: UI, preview and availability

Status: resolved
Blocked by: 02, 04

- Search/request box accepts an Audible URL or a bare ASIN and shows a preview: cover, title, authors, narrators, abridged, region. An unresolved ASIN shows the placeholder with "details will fill in".
- Square cover shelf on Home and a Library tab for audiobooks, following the D029/D030 patterns; the "Audiobookshelf: Available" tag opens the item through its `external_url`.
- A Request for an ASIN the library already holds returns `409 already_available`, as for other types.
- Library items without an ASIN surface in the suggested-matches review (title/author) for an admin to confirm.
- Accessibility and responsive layout take precedence over pixel-matching (AGENTS.md).

Acceptance: request-by-ASIN to Available works end to end in the web app against fixtures.

## Answer

Built. Home's request box now takes a bare ASIN or an Audible link (it is a text field, not `type=url`) and previews the audiobook through the existing catalog dialog: cover, author, narrators, abridged/unabridged, series, length and Audible region, or "Details will fill in" for an unresolved ASIN, which can still be requested. Home has a square "New audiobooks in your libraries" shelf and Library has an Audiobooks tab; `/library/items`, `/library/items/{id}` and `/library/recent` accept `audiobook`. The Audiobookshelf adapter now supplies covers (`ArtworkSource`, `/api/items/<id>/cover`, key in a header only). Library items with no issue kinds (audiobooks, for now) hide "Report an issue". The `409 already_available` rule is the shared one, unchanged.

Verified: Playwright specs (preview, unresolved, link, available, a11y, Library tab) and the whole suite pass; Postgres integration tests (run against a throwaway container) cover request-by-ASIN to Available through `PersistSnapshot`, an ASIN-less item producing only a pending suggestion, and an item resolved after the library synced being Available at once.

Differences from the ticket: suggested matches compare the normalised **title only**, not the author (`candidateMatch` is shared and media-agnostic, and `inventory.Item` has no author field). The adapter therefore leaves `Year` empty on purpose (Audible's release year is the recording's, Audiobookshelf's `publishedYear` usually the book's), so the candidate is title-only rather than hidden by a year mismatch. Not run: a real browser session against a live server (all browser tests use mocked API responses).
