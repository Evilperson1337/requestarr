# 01: One place that knows what a role may do

**What to build:** Every question of the form "may this person do that?" is answered by one role module, and the server tells the web app what the signed-in person can do, so the interface never compares role names. Nothing changes for anyone: the three roles behave exactly as today, and `superuser` is not stored yet. This is the prefactor that the rest of the work stands on. (Spec: Implementation Decisions, role model and session answer; stories 54, 56.)

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] A role module defines the ranking of the four roles and what each may do (request, manage requests, manage the system); the three stored roles map onto it unchanged
- [ ] The HTTP access check, the request, issue and notification services and the store's role checks ask the module instead of comparing role strings; a search of production code finds no remaining comparison against a role name outside the module
- [ ] The session endpoint returns the role and its capabilities (request, manage requests, manage system), documented in the OpenAPI contract in the same change
- [ ] The web app decides navigation, Settings, Issues, Requests, the request modals and the Get started page from the capabilities and no longer compares role names
- [ ] An automated check fails when production Go code or the web app compares a role name outside its allowed places, in the style of the existing slug and secret-name checks
- [ ] The existing role matrix and Playwright suites pass without edits to their expectations; `scripts/check.sh` passes
