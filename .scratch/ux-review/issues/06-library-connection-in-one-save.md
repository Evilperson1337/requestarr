# 06: A library connection is one form with one save

**What to build:** Connecting a library is one form: address, username where needed, and the credential, saved together and applied at once. Today the address is saved, the card says it "needs token", and the credential is saved separately. (UX-007.)

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] One form per library holds the address, the username where the library takes one, and the write-only credential; one "Save and apply" saves what changed; a credential field left empty keeps the saved one; "Clear stored value" still works
- [x] The address and credential are written atomically (one server request, one transaction): either both apply or neither does, and the error says which part failed; no secret is ever echoed, logged or kept in the page after submit
- [x] "Test connection" tests the values just saved, and its result is shown with the form; an address set by an environment variable stays read-only and the credential can still be saved
- [x] The existing endpoints keep working for current clients; the API and OpenAPI describe the combined request; the contract test passes
- [x] Tests cover: both saved; credential fails and the address is not applied; empty credential keeps the old one; environment-locked address; keyboard-only completion and accessible errors (Playwright with axe)
- [ ] Integration tests run against your own throwaway database; `scripts/check.sh` passes at the end
