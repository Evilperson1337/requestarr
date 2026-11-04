# 10: Search Audiobooks by name

Parent: `.scratch/name-search/spec.md`

**What to build:** An Audiobooks type on Home, explicit submit only. Audnexus has no title search, so the Audible package searches the Audible catalog (the same host its resolver already uses) in the admin's default marketplace, with no per-request region picker. The ref is an Audible product URL or bare ASIN that the existing canonicalization accepts. Each row shows narrator, runtime and abridged or unabridged so editions can be told apart; multi-part or series parents are labelled "Series / multi-part", not hidden. Several editions of one title appear as separate rows, because two ASINs are two Requests (D031), and the UI copy says so. The endpoint is undocumented, so results are cached, the call has a bounded timeout, and failures show as errors with the paste hint. Re-read the audiobook research and re-check the endpoint before shipping, since it is the riskiest provider. The status query is extended to match the ASIN held by Audiobookshelf.

**Blocked by:** 08

**Status:** ready-for-agent

- [ ] The Audiobooks type appears only when the audiobook resolver and marketplace are configured, and searches on explicit submit only
- [ ] Rows show narrator, runtime and abridged or unabridged; multi-part parents carry the "Series / multi-part" label
- [ ] The contract test asserts the Audible canonicalization accepts every fixture result's ref
- [ ] Two editions of the same book appear as two rows, and the UI copy explains it
- [ ] Marketplace comes from the admin default; no region control is shown
- [ ] Timeout, non-200 and malformed responses show an error, never "no results"
- [ ] An audiobook in Audiobookshelf shows "In library" by ASIN; a requested one shows "Requested"
- [ ] The endpoint's undocumented status, observed behavior and date checked are recorded in the provider package documentation
- [ ] `make fmt test vet web-check`, `make test-integration` and the browser tests pass
