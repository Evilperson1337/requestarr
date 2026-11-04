# 10: Search Audiobooks by name

Parent: `.scratch/name-search/spec.md`

**What to build:** An Audiobooks type on Home, explicit submit only. Audnexus has no title search, so the Audible package searches the Audible catalog (the same host its resolver already uses) in the admin's default marketplace, with no per-request region picker. The ref is an Audible product URL or bare ASIN that the existing canonicalization accepts. Each row shows narrator, runtime and abridged or unabridged so editions can be told apart; multi-part or series parents are labelled "Series / multi-part", not hidden. Several editions of one title appear as separate rows, because two ASINs are two Requests (D031), and the UI copy says so. The endpoint is undocumented, so results are cached, the call has a bounded timeout, and failures show as errors with the paste hint. Re-read the audiobook research and re-check the endpoint before shipping, since it is the riskiest provider. The status query is extended to match the ASIN held by Audiobookshelf.

**Blocked by:** 08

**Status:** ready-for-agent

- [x] The Audiobooks type is offered only when the server lists it in /search/types (browser test); it is registered always, since an unset marketplace means US; it searches on explicit submit only (browser test)
- [x] Rows show narrator, runtime and abridged or unabridged; multi-part parents carry the "Series / multi-part" label (Go test on the recorded live fixture; browser test for rendering)
- [x] The contract test asserts the Audible canonicalization accepts every fixture result's ref (TestSearchRefsAreCanonical)
- [x] Two editions of the same book appear as two rows (fixture has two "Hail Mary" ASINs), and the search hint explains it (browser test)
- [x] Marketplace comes from the admin default (Go test with UK); no region control is shown (browser test)
- [x] Non-200 and malformed responses are errors in Go tests; the UI shows the error with the paste hint (browser test, mocked 503). Timeout is the shared fetch/search budget, not separately tested
- [x] An audiobook in Audiobookshelf shows "In library" by ASIN and a requested one "Requested" (real-PostgreSQL test TestSearchLocalStatesAudiobookByASINIntegration)
- [x] The endpoint's undocumented status, observed behavior and date checked (2026-11-04, live, HTTP 200) are recorded in internal/providers/metadata/audible/README.md
- [x] `make fmt test vet web-check`, `make test-integration` and the browser tests pass (except the known home-recent limit failure)
