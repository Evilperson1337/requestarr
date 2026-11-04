# 06: Bookmarkable searches, Back, and focus return

Parent: `.scratch/name-search/spec.md`

**What to build:** A search lives in the Home URL (`?q=&type=`), so Back after opening a preview restores the same results and a search can be bookmarked or sent to a housemate. The result list and its scroll position stay mounted while the modal is open, and closing the modal returns focus to the result row that opened it. Typing replaces the history entry after the first push, so Back is not flooded. Keyboard and screen-reader behavior is covered end to end: the radio group is arrow-key operable, results are reachable in order, and the live region reports "12 results for dune", "Searching…" and "Search unavailable".

**Blocked by:** 03

**Status:** done

- [x] Loading Home with `?q=dune&type=movie_tv` runs that search and selects that type
- [x] An unknown or unconfigured `type` in the URL falls back to a valid one without an error
- [x] Back after opening and closing a preview restores the query, type and results
- [x] Typing does not create one history entry per keystroke
- [x] Closing the modal (Cancel, Close, Escape) returns focus to the result row, and the scroll position is unchanged
- [x] Browser tests cover keyboard-only use of the radio group, result list and modal, and assert the live-region text for loading, results, empty and error
- [x] A pasted link is not written to the URL as `q`
- [x] `make fmt test vet web-check` and the browser tests pass
