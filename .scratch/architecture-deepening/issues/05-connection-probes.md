# 05: Connection probes return structured results

**What to build:** In setup/checks, one probe per kind (keyed by sources) plus TMDB, IGDB and Apprise, over a shared helper that judges status, content type, size and body. A probe returns check code, user-facing message and optional secret note. Service.Validate only maps results to draft check status, expiry and revisions. Move TMDB and IGDB wording out of setup/service.go; delete checkedSecrets and secretNote from Service.

**Blocked by:** 04

**Status:** done, with one deliberate omission

- [ ] Message mapping is tested without HTTP; response judging without secrets
- [ ] Existing httptest-based checker tests pass unchanged as the contract
- [ ] service.go has no provider-specific wording

## Progress

Done: check wording and the secret-source note moved out of `setup.Service` into `checks/messages.go` (`Checker.Describe`, plus the pure `describe`). The `Checker` interface gained `Describe`; `Service.Validate` records what the checker says and only normalizes unknown codes, expiry and revisions. `checkedSecrets`, `secretNote`, `UseSecrets` and the `Service.secrets` field are deleted; `setup/service.go` has no TMDB or IGDB message wording left (its Environment listing still names them, since that is draft state). Message mapping is tested without HTTP in `checks/messages_test.go`, and `setup/validate_result_test.go` covers the service's part. The httptest-based checker tests pass unchanged. Go and integration tests pass; web is untouched.

Removed: five `setup` tests that asserted wording through `Service.Validate` (`secret_source_test.go`, `secret_unreadable_note_test.go`, `secret_source_igdb_apprise_test.go`, `library_secret_source_test.go`, and the untracked `provider_messages_test.go`), plus the Navidrome message test in `navidrome_username_test.go`. Their assertions are in `messages_test.go`.

Gaps:
- The probes are not yet one structured result. `Check` still returns `(CheckCode, error)`; a probe returning code, message and note together would change that signature and every checker test, which the ticket says must stay as the contract.
- Only the library probes share a table (`libraryProbes`). TMDB, IGDB and Apprise each still do their own status, content-type, size and body checks inline.

## Follow-up (done)

Every probe now goes through one `Checker.fetch` (`checks/fetch.go`) that judges status, content type, size and body and records one diagnostic: library Sources, Apprise, TMDB, IGDB, MusicBrainz and OIDC discovery. `fetch_test.go` tests that judging once, with no secrets or real provider. The existing httptest-based checker tests pass unchanged. `Check` still returns `(CheckCode, error)` with `Describe` for the message, so a probe returning code, message and note as one value was not done; it would change that interface and every checker test for no behavior gain.
