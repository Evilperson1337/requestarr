# 07: Decisions, glossary and guides

**What to build:** The decisions, vocabulary and guides describe the four roles and the group mapping so an operator can set them up without reading the code. (Spec: Implementation Decisions, decisions and docs.)

**Blocked by:** 03, 05, 06

**Status:** ready-for-agent

- [ ] A new decision in the decisions record covers the four roles, the superuser-only list, the group mapping rules, the scopes default and the upgrade, and amends D035 (admin and superuser only by an explicit mapping or a superuser) and D040 (the local administrator is a superuser)
- [ ] The glossary defines Superuser and Admin and updates Requester, Viewer and Local administrator
- [ ] The Unraid and sign-in guides explain the roles and how to set up groups, including a Keycloak group membership mapper that puts `groups` in the ID token, and what the Test warning means
- [ ] Documents that still say the local administrator is an admin, that no identity is ever made admin, or that Settings is admin-only are corrected; the deploy-files check passes
- [ ] `scripts/check.sh` passes
