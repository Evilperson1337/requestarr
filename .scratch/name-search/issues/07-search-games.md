# 07: Search Games by name

Parent: `.scratch/name-search/spec.md`

**What to build:** A Games type on Home that searches as you type and opens the usual game preview, where platforms are still chosen (D009). The IGDB package implements the searcher. Results use limit and offset paging (not fetch-and-slice), and carry the game's **slug**, because the IGDB resolver identifies a game by slug in its URL, so the ref is `https://www.igdb.com/games/<slug>`. The user's text is escaped for the query language (backslash and double quote) or rejected, never interpolated raw. Main games, remakes, remasters, ports and expanded games are shown; DLC, updates and bundles are hidden as a fixed rule. The field holding the kind is `game_type` (it replaced the deprecated `category`): verify it against the live API instead of guessing the enum, and record what was found. A small concurrency cap stays under IGDB's 4 requests per second and 8 open requests, and a 429 maps to the busy error. The row shows a short platform hint only. The batched status query from ticket 04 is extended to match IGDB identifiers from RomM.

**Blocked by:** 03

**Status:** ready-for-agent

- [ ] The Games type appears only when IGDB is configured
- [ ] Results show title, year, cover and a short platform hint; picking one opens the existing game preview with platform selection
- [x] The contract test asserts the IGDB resolver accepts every fixture result's ref (igdb TestEveryFixtureResultRefIsAcceptedByTheResolver runs Resolve on each ref and checks the slug IGDB is asked for; fixture-level, not live)
- [x] A query containing quotes or backslashes cannot change the IGDB query (fixture test with a hostile string) (TestSearchEscapesHostileText...; fixture-level)
- [x] DLC, updates and bundles are absent; remakes, remasters, ports and expanded games are present (fixture test) (TestSearchShowsGamesAndRemakes...; rule is also in the IGDB where clause, unverified live)
- [x] Paging uses offset and reaches beyond what a fetch-and-slice would (TestSearchPagesWithOffset...: page 3 sends offset 40, 21st row sets hasMore)
- [x] A 429 or exhausted concurrency shows the busy message, not an empty list (igdb tests for 429/503 (one attempt, ErrBusy) and the cap of 3; the busy message in the UI is the existing search error path (ticket 03 test))
- [ ] An item in RomM shows "In library"; a requested game shows "Requested"
- [ ] The verified game-kind field name and how it was verified are noted in the provider package documentation
- [ ] `make fmt test vet web-check`, `make test-integration` and the browser tests pass

## Implementation notes (agent)

Left unticked on purpose:
- "Games type appears only when IGDB is configured": wired in `internal/app/app.go` (client ID and secret both set); no test covers that wiring, and the Playwright test mocks `/search/types`.
- "Results show title, year, cover and a short platform hint; picking one opens the game preview": covered only by `web/tests/search-games.spec.ts` with mocked API responses.
- "In library / Requested": depends on ticket 04 (not built here).
- "Verified game-kind field": NOT verified live; no IGDB credentials in the environment. Implemented against documented `game_type`, recorded as unverified in `internal/providers/metadata/igdb/README.md`.
- `make test-integration` was not run (needs the dev stack); `make fmt test vet web-check` pass and `make web-test` passes except the known `home-recent` failure.
