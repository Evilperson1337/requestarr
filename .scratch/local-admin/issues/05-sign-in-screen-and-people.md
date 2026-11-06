# 05: The sign-in screen, and the local administrator among people

**What to build:** The signed-out screen offers the right ways in, and the Users list treats the local administrator correctly. (Spec stories 18-22, 39-43; D040.)

**Blocked by:** 01, 03

**Status:** resolved

- [x] With OIDC enabled the signed-out screen shows "Sign in with <provider name>" as the main action and a smaller "Administrator sign-in" link that opens the local form; with OIDC not configured or disabled it shows only the local form; it explains what Requestarr is and how to sign in
- [x] The local form is always reachable at its own address, including when the OIDC configuration is broken (tested with a failing configuration)
- [x] The Users list marks the local administrator, locks its role to admin, and cannot demote or remove it; the existing last-admin protection still applies; a person who signed in through OIDC can be promoted by an admin exactly as before
- [x] New OIDC sign-ins still receive the default role chosen in Settings
- [x] Tests: Playwright with axe for the signed-out screen in each state, the forced-change first sign-in, and the Users list; unauthenticated, viewer, requester and admin paths; no OIDC state is exposed to an unauthenticated caller beyond whether it is enabled and its provider name
- [x] `scripts/check.sh` passes (run on the integrated branch with the PostgreSQL integration tests and the whole Playwright suite)
