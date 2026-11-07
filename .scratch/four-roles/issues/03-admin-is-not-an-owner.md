# 03: Admin is a day-to-day manager, not an owner

**What to build:** An admin runs requests, issues, matches and people, and cannot change how people sign in or where the credentials live. Only a superuser can, and the interface shows each role only what it can use. (Spec stories 2-10, 16-18, 50-52; Implementation Decisions: access levels, superuser-only, Settings page.)

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] The route table has a superuser access level; the role matrix test asserts every route for viewer, requester, admin and superuser, with the 401 and 403 cases
- [ ] Superuser only: Authentication settings (state, save, test, enable, discard), local administrator management, the stored-secret endpoints, integration and provider connection settings and credentials, the notification gateway key, and role changes to or from admin and superuser; each route is listed in the matrix test
- [ ] Admin keeps request approval, denial and cancel and the awaiting count, issues, matches, operational status and activity (read only), the approval policy, the default role for new people, and the Users list
- [ ] An admin sees Settings with Activity, Matches and Users; Integrations and Authentication are superuser-only and absent (not disabled) for an admin; Get started and its navigation entry are superuser-only
- [ ] A refused action names the role needed
- [ ] Playwright with axe at desktop and phone widths covers the Settings tabs and navigation for admin and superuser
- [ ] The OpenAPI contract is updated with the access levels; `scripts/check.sh` passes
