# 02: Show the TMDB attribution notice

Parent: `.scratch/name-search/spec.md`

**What to build:** TMDB's API terms make attribution a condition of the licence: the TMDB logo and the statement that the product is "not endorsed, certified, or otherwise approved by TMDB". Today the UI shows neither (only a provider logo in the Settings integration list). Add them where a person who sees TMDB titles can find them, for example a footer or an About area reachable from every page. This must land before name search for Movies & TV is released, because search puts far more TMDB content on screen.

**Blocked by:** None (can start immediately)

**Status:** needs-review

- [ ] The official TMDB attribution logo (not the settings-list provider mark, unless it is confirmed to be the official asset) and the non-endorsement sentence are visible from the pages that show TMDB metadata
- [ ] The logo has a text alternative and the notice has sufficient contrast in light and dark themes
- [x] The layout still works at phone width and with the bottom bar
- [x] A browser test asserts the notice is present
- [ ] `make fmt test vet web-check` pass
