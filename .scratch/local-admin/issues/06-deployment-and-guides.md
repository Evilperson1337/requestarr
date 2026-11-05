# 06: Deployment files and guides for the new first run

**What to build:** Installing on Unraid no longer needs an identity provider. The template and compose example make OIDC an optional override and offer an initial administrator password, and the guides walk from first sign-in to a working OIDC setup. (Spec stories 45-49; D040.)

**Blocked by:** 02, 04, 05

**Status:** ready-for-agent

- [ ] The Unraid template's OIDC issuer, client ID and client secret fields become optional advanced overrides (not required, no default; the secret masked), with descriptions that say the environment overrides anything saved in Settings, Authentication; a new masked optional field supplies the initial administrator password with no default; the compose example and example environment file follow
- [ ] The deployment-file check is extended and passes: the new fields' flags, no secret default, the template and compose variable names matching each other and the configuration code, and mutation checks in the existing style
- [ ] `docs/UNRAID.md` is rewritten around signing in first: find the one-time password in the log, sign in, choose a password, configure OIDC in Settings, Authentication with a Keycloak worked example (client type, redirect address, scopes), test it, then promote the first OIDC person in Settings, Users; it keeps the HTTPS requirement and explains why a password needs it, and documents `reset-admin-password`
- [ ] `docs/FIRST_RUN.md`, `docs/OIDC_AUTH.md`, `docs/UPGRADE_AND_BACKUP.md` and `docs/MANUAL_TESTING.md` (the real-server checklist) describe the new flow, including that an upgraded instance keeps its environment-configured OIDC and can create the local administrator from Settings
- [ ] No document, template text or script mentions the removed claim flow or `setup-token` except a historical note in a decision record
- [ ] `scripts/check.sh` passes
