# 04: The Authentication page in Settings

**What to build:** Settings, Authentication is where an admin manages how people sign in: a Local administrator card and an OpenID Connect card, with every environment conflict explained. (Spec stories 8, 23-38, 41; D040.)

**Blocked by:** 01, 03

**Status:** ready-for-agent

- [ ] A new Authentication section in Settings follows the existing Settings layout and integration-card pattern, with two cards: Local administrator (status, change password, "Create local administrator" on an instance that has none, disable and enable) and OpenID Connect (enable switch, provider name, issuer, client ID, write-only client secret with the usual "stored encrypted, never shown again" wording, the redirect address with a copy button, a Test button, and a current-state line)
- [ ] Creating the local administrator from the page shows the generated password once and never again; the password is never shown elsewhere; disabling the local administrator is refused unless another admin exists, with a clear message
- [ ] Every state from the state function has its own message: environment only (fields read-only, "Set by environment"); environment overriding saved settings (a visible warning naming what differs or is ignored, with "Discard saved settings"); environment incomplete (an error naming the missing variable); no master key (the secret field disabled with the explanation); a failing test says what failed and keeps the previous configuration
- [ ] The page is reviewed with the design skill (`/design-taste-frontend`); its findings are applied where they fit the existing system (hierarchy, spacing, states, polish) and the review notes, including anything deliberately not applied and why, are recorded in the ticket's Comments
- [ ] Keyboard-only completion, accessible errors and status announcements; Playwright with axe at desktop and phone widths for each state above
- [ ] The Get started checklist's sign-in item becomes "OIDC configured" and is optional
- [ ] `scripts/check.sh` passes
