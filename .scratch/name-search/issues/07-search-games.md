# 07: Search Games by name

Parent: `.scratch/name-search/spec.md`

**What to build:** A Games type on Home that searches as you type and opens the usual game preview, where platforms are still chosen (D009). The IGDB package implements the searcher. Results use limit and offset paging (not fetch-and-slice), and carry the game's **slug**, because the IGDB resolver identifies a game by slug in its URL, so the ref is `https://www.igdb.com/games/<slug>`. The user's text is escaped for the query language (backslash and double quote) or rejected, never interpolated raw. Main games, remakes, remasters, ports and expanded games are shown; DLC, updates and bundles are hidden as a fixed rule. The field holding the kind is `game_type` (it replaced the deprecated `category`): verify it against the live API instead of guessing the enum, and record what was found. A small concurrency cap stays under IGDB's 4 requests per second and 8 open requests, and a 429 maps to the busy error. The row shows a short platform hint only. The batched status query from ticket 04 is extended to match IGDB identifiers from RomM.

**Blocked by:** 03

**Status:** ready-for-agent

- [ ] The Games type appears only when IGDB is configured
- [ ] Results show title, year, cover and a short platform hint; picking one opens the existing game preview with platform selection
- [ ] The contract test asserts the IGDB resolver accepts every fixture result's ref
- [ ] A query containing quotes or backslashes cannot change the IGDB query (fixture test with a hostile string)
- [ ] DLC, updates and bundles are absent; remakes, remasters, ports and expanded games are present (fixture test)
- [ ] Paging uses offset and reaches beyond what a fetch-and-slice would
- [ ] A 429 or exhausted concurrency shows the busy message, not an empty list
- [ ] An item in RomM shows "In library"; a requested game shows "Requested"
- [ ] The verified game-kind field name and how it was verified are noted in the provider package documentation
- [ ] `make fmt test vet web-check`, `make test-integration` and the browser tests pass
