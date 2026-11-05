# 04: Claim the first admin with just the token

**What to build:** A person who signs in before any admin exists sees their own issuer and subject on the claim page as a ready-to-run `setup-token` command, runs it on the server, and pastes only the token. They no longer visit the identity provider's admin console to find a user ID, and no longer retype values the server already holds. (UX-002; D037. This is security-sensitive: negative tests are required.)

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] While first-admin setup is unclaimed, the signed-in caller (and only the caller) can see their own verified issuer and subject; once an admin exists, or for any other caller, nothing identity-related is exposed; the OpenAPI describes it
- [x] The claim page shows the exact command for the container (`docker exec <container-name> /requestarr setup-token --subject '<their subject>' --stdout`, issuer included when it differs from the configured one) with a copy control, then a single token field
- [x] The claim endpoint takes the token only and derives issuer and subject from the verified session; a request that still sends issuer and subject must match the session or is rejected
- [x] `setup-token` defaults `--issuer` to the configured `REQUESTARR_OIDC_ISSUER` (an explicit flag still works); the existing validation and the token's 30-minute single-use binding are unchanged
- [x] The startup log line (added earlier) is updated to say that the signed-in person's claim page shows the command
- [x] Negative tests: wrong token, expired token, token issued for a different subject, a different signed-in user, a second claim after success, a viewer reading identity after setup is complete; none changes or leaks anything
- [x] `docs/UNRAID.md` section 6, `docs/SETUP_WIZARD.md` and the page's text describe the new flow; the Playwright claim test is updated
- [x] D037 is written in `docs/DECISIONS.md`
- [ ] Integration tests run against your own throwaway database; `scripts/check.sh` passes at the end
