# 09: Search Songs by name

Parent: `.scratch/name-search/spec.md`

**What to build:** A Songs type on Home, explicit submit like Albums. The MusicBrainz searcher gains a recording search whose ref is the recording URL the resolver accepts (D020). The same recording appears under many releases, so the list shows one row per recording with artist, title, length and the first release, not the list of releases. It reuses the query escaping, lower-priority queueing and explicit-submit UI from ticket 08. The status query is extended to songs, including library identifiers that need alias matching.

**Blocked by:** 08

**Status:** ready-for-agent

- [ ] The Songs type appears when MusicBrainz is available and searches on explicit submit only
- [ ] A recording that appears on several releases yields one row
- [ ] Each row shows artist, title, length and first release
- [ ] The contract test asserts the MusicBrainz resolver accepts every fixture result's ref
- [ ] Hostile query text is handled as in Albums (fixture test)
- [ ] A song held in a library shows "In library"; a requested song shows "Requested"
- [ ] `make fmt test vet web-check`, `make test-integration` and the browser tests pass
