# 09: Search Songs by name

Parent: `.scratch/name-search/spec.md`

**What to build:** A Songs type on Home, explicit submit like Albums. The MusicBrainz searcher gains a recording search whose ref is the recording URL the resolver accepts (D020). The same recording appears under many releases, so the list shows one row per recording with artist, title, length and the first release, not the list of releases. It reuses the query escaping, lower-priority queueing and explicit-submit UI from ticket 08. The status query is extended to songs, including library identifiers that need alias matching.

**Blocked by:** 08

**Status:** needs-review

- [x] The Songs type appears when MusicBrainz is available and searches on explicit submit only. Verified: `app.go` registers `song` next to `album` (always on, no key needed); Playwright `web/tests/search-songs.spec.ts` (mocked API) shows no request while typing and one on Enter
- [x] A recording that appears on several releases yields one row. Verified: fixture test (`song_search_test.go`) with a three-release recording gives one row; a live probe showed MusicBrainz already nests releases under one recording
- [x] Each row shows artist, title, length and first release. Verified: fixture test on the subtitle `Artist · m:ss · earliest dated release` plus year
- [x] The contract test asserts the MusicBrainz resolver accepts every fixture result's ref (`TestEverySongFixtureRefIsAcceptedByTheResolver`)
- [x] Hostile query text is handled as in Albums (fixture test). Both searches share `termsQuery`
- [ ] A song held in a library shows "In library"; a requested song shows "Requested". Partly verified against real PostgreSQL (`search_local_music_integration_test.go`): Requested always; In library when the library stores the recording id (Navidrome, Plex `mbid`) or the song has aliases (recorded for an open Request). Not satisfied for a song nobody has requested that Plex holds under a release-track id: no catalog row or aliases, so it shows no badge (unknown)
- [x] `make fmt test vet web-check`, `make test-integration` (fresh throwaway database) and `make web-test` pass; the only failure is the known pre-existing tests/home-recent.spec.ts limit=48 vs 12
