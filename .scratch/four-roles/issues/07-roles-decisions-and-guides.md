# 07: Decisions, glossary and guides

**What to build:** The decisions, vocabulary and guides describe the four roles and the group mapping so an operator can set them up without reading the code. (Spec: Implementation Decisions, decisions and docs.)

**Blocked by:** 03, 05, 06

**Status:** ready-for-agent

- [x] A new decision in the decisions record covers the four roles, the superuser-only list, the group mapping rules, the scopes default and the upgrade, and amends D035 (admin and superuser only by an explicit mapping or a superuser) and D040 (the local administrator is a superuser)
- [x] The glossary defines Superuser and Admin and updates Requester, Viewer and Local administrator
- [x] The Unraid and sign-in guides explain the roles and how to set up groups, including a Keycloak group membership mapper that puts `groups` in the ID token, and what the Test warning means
- [x] Documents that still say the local administrator is an admin, that no identity is ever made admin, or that Settings is admin-only are corrected; the deploy-files check passes
- [x] `scripts/check.sh` passes

## Comments

- D041 (docs/DECISIONS.md) read back against the code and OpenAPI: roles, 19 superuser-only operations (matches `TestSuperuserOnlyRoutesAreExactlyThese` in ticket 03), setting names and defaults, scopes default and migration, upgrade; D035 and D040 carry an amendment note in place.
- Glossary: Superuser, Admin, Local administrator were already updated by ticket 02; I added Superuser-only and Group mapping and updated Requester and Viewer.
- Guides: docs/UNRAID.md (roles table, Scopes field, Keycloak `groups` client scope with a Group Membership mapper, full group path off, ID token on, the Test warning explained, troubleshooting row), docs/OIDC_AUTH.md, docs/FIRST_RUN.md. UI labels checked in `web/src/components/AuthenticationSettings.tsx` and the badge text "Always a superuser".
- Corrected: AGENTS.md rules 5 and 12, README, V1_SCOPE, SECURITY, TESTING, CODING_STANDARDS (route table and role matrix), UPGRADE_AND_BACKUP, ARCHITECTURE, UI_UX, REQUESTS_UI, INTEGRATIONS.
- `scripts/check.sh` passed (fmt, test, vet, integration on requestarr_t07, deploy-files check, tsc, 359 Playwright tests).
