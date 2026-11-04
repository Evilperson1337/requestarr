# 08: Search Albums by name

Parent: `.scratch/name-search/spec.md`

**What to build:** An Albums type on Home that searches only when the person presses Enter or a Search button (not as they type), because MusicBrainz allows about 1 request per second per IP for the whole instance, shared with previews, tracklists and alias lookups. The MusicBrainz package implements a release-group search whose ref is the release-group URL the resolver already accepts (D020). Query text is escaped, or the escaping query mode is used, so a typed `*`, `:` or `"` cannot alter or break the query. Primary type Album is the default, with an "Include singles & EPs" toggle (off by default); rows show type, artist and year. Search takes lower priority than a resolve in the shared spacer, has a short queue depth, and gives up its slot when the browser cancels, so someone clicking a result to preview never waits behind searches. Artwork must not be on the search critical path (use the artwork URL already derivable from the identifier, with no extra call). This ticket introduces the explicit-submit path in the Home UI. The status query from ticket 04 is extended to albums, including Plex's MusicBrainz identifiers, which are release IDs rather than release-group IDs and need alias matching.

**Blocked by:** 03

**Status:** ready-for-agent

- [ ] The Albums type appears only when MusicBrainz is available and searches on Enter or the button only
- [ ] Default results are albums only; the toggle adds singles and EPs; each row shows its type and year
- [ ] The contract test asserts the MusicBrainz resolver accepts every fixture result's ref
- [ ] Hostile query text (quotes, colons, asterisks, parentheses) neither errors nor changes the query semantics (fixture test)
- [ ] With searches queued, a resolve is served first and a cancelled search frees its slot (test with a fake clock or spacer)
- [ ] MusicBrainz busy or timeout shows an error with the paste hint, never "no results"
- [ ] An album held in Navidrome or Plex shows "In library", including when the library stores a release ID
- [ ] Explicit-submit behavior works with the keyboard and the live region (browser test)
- [ ] `make fmt test vet web-check`, `make test-integration` and the browser tests pass
