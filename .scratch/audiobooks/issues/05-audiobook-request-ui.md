# Request audiobook: UI, preview and availability

Status: ready-for-agent
Blocked by: 02, 04

- Search/request box accepts an Audible URL or a bare ASIN and shows a preview: cover, title, authors, narrators, abridged, region. An unresolved ASIN shows the placeholder with "details will fill in".
- Square cover shelf on Home and a Library tab for audiobooks, following the D029/D030 patterns; the "Audiobookshelf: Available" tag opens the item through its `external_url`.
- A Request for an ASIN the library already holds returns `409 already_available`, as for other types.
- Library items without an ASIN surface in the suggested-matches review (title/author) for an admin to confirm.
- Accessibility and responsive layout take precedence over pixel-matching (AGENTS.md).

Acceptance: request-by-ASIN to Available works end to end in the web app against fixtures.
