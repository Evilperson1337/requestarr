# 03: Admin is a day-to-day manager, not an owner

**What to build:** An admin runs requests, issues, matches and people, and cannot change how people sign in or where the credentials live. Only a superuser can, and the interface shows each role only what it can use. (Spec stories 2-10, 16-18, 50-52; Implementation Decisions: access levels, superuser-only, Settings page.)

**Blocked by:** 02

**Status:** done

- [x] The route table has a superuser access level; the role matrix test asserts every route for viewer, requester, admin and superuser, with the 401 and 403 cases
- [x] Superuser only: Authentication settings (state, save, test, enable, discard), local administrator management, the stored-secret endpoints, integration and provider connection settings and credentials, the notification gateway key, and role changes to or from admin and superuser; each route is listed in the matrix test
- [x] Admin keeps request approval, denial and cancel and the awaiting count, issues, matches, operational status and activity (read only), the approval policy, the default role for new people, and the Users list
- [x] An admin sees Settings with Activity, Matches and Users; Integrations and Authentication are superuser-only and absent (not disabled) for an admin; Get started and its navigation entry are superuser-only
- [x] A refused action names the role needed
- [x] Playwright with axe at desktop and phone widths covers the Settings tabs and navigation for admin and superuser
- [x] The OpenAPI contract is updated with the access levels; `scripts/check.sh` passes


## Comments

Checked by running real code. The Go checks drive the real route table and handlers; the Playwright specs use mocked HTTP, which is what the spec's seam 3 intends, and the capabilities they are fed are produced by the real role module (`TestAuthMeReturnsTheRoleAndItsCapabilities`).

- Route table: `accessSuperuser` in `internal/httpapi/access.go` (checked with `Role.CanManageAccess`). The role matrix (`role_matrix_test.go`) runs every mutating route for unauthenticated, viewer, requester, admin and superuser (401, 403, success, plus CSRF/origin); rows for the superuser-only routes carry policy `superuser`, and `role_coverage_test.go` fails when a row and the route's access disagree.
- Superuser-only list: `TestSuperuserOnlyRoutesAreExactlyThese` spells out all 19 routes (authentication state/save/test/enable/discard, local administrator status/create/disable, stored secrets list/set/clear, library connections list/save/remove/test, IGDB client ID get/save, Get started checklist get/dismiss). The notification gateway key is a stored secret (`apprise_config_key`), so `/admin/secrets` covers it. Role changes to or from admin and superuser are refused by `Role.CheckRoleChange` (ticket 02; `PUT /admin/users/{userID}/role` itself stays an admin route because an admin may still move requesters and viewers).
- Admin keeps: `TestAdminKeepsTheDayToDayRoutes` (users, role change, default role, approval policy, status, job runs, matches, sync, awaiting count, issue status). Request transitions are decided in the requests service and unchanged.
- Web: Settings filters its tabs by the new `manageAccess` capability (fourth field of the session answer, in `domain.Capabilities`, `/auth/me`, OpenAPI and `web/src/api.ts`); Integrations and Authentication are not rendered, nor fetched, for an admin, and a deep link to them lands on the first tab. Get started (entry, fetch and page) follows `manageAccess`. `web/tests/settings-roles.spec.ts` asserts tabs, absent navigation entries, the Get started refusal and that no superuser-only endpoint is requested by an admin.
- Refusal text: 403 `forbidden` says "the superuser role is required" or "the admin role is required"; the matrix asserts the message names the role for every refused row.
- Axe: `settings-roles.spec.ts` audits every Settings tab as admin and superuser at 1280 and 390 px.
- Contract: `x-required-role: superuser` on the 19 operations, an access-levels paragraph, `manageAccess` in the session schema; `TestOpenAPINamesTheSuperuserOnlyOperations` keeps the marks and the route table in step.
- `scripts/check.sh` passed on a database of its own (`requestarr_t03`) after merging `feat/four-roles`.
- Existing specs that exercise Integrations, Authentication or Get started now sign in as a superuser (only the `/auth/me` fixture changed, no expectation).
- Known gap: an admin has no Sync now button, because it lives on the library cards in Integrations; `POST /admin/sources/{source}/sync` is still an admin route, so a later ticket may add it to Activity.
- For ticket 07 (docs): CODING_STANDARDS route table and role matrix need the superuser rows listed above; the glossary needs "superuser-only"; messages are "the superuser role is required" / "the admin role is required" (the old "administrator role required" is gone).
