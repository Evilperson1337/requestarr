# 18: Requests page controls

**What to build:** The Requests page offers search, filters, sort and paging, all in the URL, plus the cancel and leave actions.

**Blocked by:** 17 Request search, filter, sort and cursor pagination (API), 08 Cancel and leave a Request

**Status:** done

- [x] Controls reflect and update the URL so views are bookmarkable
- [x] My Requests remains a filter on the shared page
- [x] Cancel and leave appear for the right roles
- [x] Poster and square artwork shapes are preserved
- [x] Keyboard operable, labelled, and usable at phone widths

**Delivered:** Bookmarkable real-path navigation; title search, status chips,
media type, My Requests, sort, page size, and cursor controls. Filter changes
reset paging. First/Previous/Next and browser history work without fabricated
totals; expired cursors restart with filters preserved. First-page history
restores retain traversal membership and explicitly label last-loaded details;
Refresh starts a fresh view. Role-appropriate actions retain CSRF, versioning,
server-confirmed last-subscriber cancellation, duplicate guards, and persistent
errors. Poster/square bounds and responsive controls preserve artwork ratios.
See `docs/REQUESTS_UI.md` for behavior and `docs/TESTING.md` for test setup.

**Validation:** `make fmt`, `make test`, `make vet`, `make web-check`,
`make web-test` (12 Chromium tests), `make build`,
`make test-integration REQUESTARR_POSTGRES_PORT=5433`, and `git diff --check`
passed. Browser tests use deterministic API fakes and cover bookmarks/reloads,
history/paging stability under inserts, cursor recovery, role actions, CSRF,
conflicts, confirmation/decline, stale responses, keyboard focus, and 360px
layout/artwork. The phone screenshot was visually inspected. Independent
Standards review found no issues; one Spec pagination finding was fixed with a
regression and confirmed resolved in follow-up review.

**Remaining risks/scope:** Browser coverage is Chromium with mocked APIs, not
live OIDC/library tests. Broader page smoke coverage and CI integration remain
ticket #24. npm audit reports two pre-existing high-severity build/dev dependency
findings in Vite 8.3.0 and source-map-js 1.2.1; no unrelated dependency upgrades
were made. Playwright is dev-only; no runtime router or test dependency is added
to the shipped app.
