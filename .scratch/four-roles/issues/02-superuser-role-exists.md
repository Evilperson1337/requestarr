# 02: Superuser exists, and existing admins and the local administrator become one

**What to build:** A `superuser` role is stored and works. On upgrade every existing admin, and the built-in local administrator, becomes a superuser, so nobody loses access. The Users page offers all four roles, with an admin limited to requester and viewer, and the instance always keeps a superuser who can sign in. (Spec stories 1, 19-27, 53, 55; Implementation Decisions: schema, guards, first sign-in.)

**Blocked by:** 01

**Status:** done

- [x] A new append-only migration (up and down) widens the role checks on users and on the role-change audit to include `superuser`, adds an audit source (manual or group mapping), and sets every existing admin, the built-in account included, to `superuser`; requesters and viewers are unchanged
- [x] An integration test starts from a database holding the old roles and proves the upgrade, including the built-in account
- [x] The built-in local administrator's role is fixed at superuser and cannot be changed or removed
- [x] The last-administrator guard becomes a last-superuser guard, counting superusers who can sign in (the local administrator only while enabled), and holds under concurrent role changes (integration test)
- [x] A superuser can set anyone to any of the four roles; an admin can change a person only between requester and viewer and cannot change an admin or a superuser; nobody can change their own role; each refusal has a clear message naming the role needed
- [x] A role change still ends the person's other sessions and is audited with its source
- [x] The Users table shows the four roles, labels superusers and admins, and offers only the choices the signed-in role may make, with Playwright and axe at desktop and phone widths
- [x] The first-sign-in notice goes to every admin and superuser and names the role granted
- [x] The OpenAPI contract, the role matrix test and the glossary entries for Superuser and Admin are updated; `scripts/check.sh` passes


## Comments

All criteria checked by running real code; PostgreSQL tests ran against a database of their own on the dev Postgres.

- Migration `000040_superuser_role` (up and down): widens `users` and `admin_user_audit_events` role checks, adds `source` (`manual`, `group_mapping`) to the audit, promotes every admin including the built-in account, and adds `users_builtin_superuser` so the database itself keeps the built-in account a superuser. `TestSuperuserMigrationPromotesExistingAdminsIntegration` rolls 40 back, fills the table with old roles (built-in admin, OIDC admin, requester, viewer plus an old audit row), applies 40, and checks roles, the old audit row, the new values, the built-in constraint and the rollback.
- Built-in role fixed: `TestTheBuiltInAccountStaysASuperuserIntegration` (even a superuser cannot change it); the HTTP code stays `builtin_admin_role`.
- Last-superuser guard: `ErrLastSuperuser` (code `last_superuser`), counts superusers minus a disabled local account. `TestLastSuperuserHoldsUnderConcurrentChangesIntegration` runs ten rounds of two superusers demoting each other; `TestLastSuperuserCountsOnlyThoseWhoCanSignInIntegration` covers the disabled local account. Note: a signed-in superuser who demotes someone is always a survivor, so the guard is reached only by a stale or disabled actor; the existing last-admin tests now use `staleSuperuser`.
- Who may change which role: `Role.CheckRoleChange` (table test in `domain/role_test.go`), enforced in `ChangeUserRole` with the actor's role read under the advisory lock (`TestWhoMayChangeWhichRoleIntegration`: refusals change nothing and write no audit; sessions end; audit has the source). The HTTP answer is 403 `role_needed` with a message naming the role.
- Users table: `GET /admin/users` returns `roleChoices` per person, computed by the role module; the page renders exactly those and no role comparison was added (`TestWebAppNeverComparesRoleNames` passes). Playwright `people-local-admin.spec.ts` covers admin and superuser, with axe at desktop and phone widths. These specs use mocked HTTP, as the spec's seam 3 intends.
- First-sign-in notice: already named the role; `TestFirstSignIn...` now also asserts a superuser receives it.
- Role matrix covers `superuser`; OpenAPI (role enums, `roleChoices`, 403/409 text) and glossary (Superuser, Admin, Local administrator) updated. `scripts/check.sh` passed (fmt, tests, vet, integration, 329 Playwright tests).
- For ticket 03: `Role.CanManageAccess()` and `RolesThatManageAccess()` are the superuser-only question; sessions still return the three capabilities, so add a capability (for example `manageAccess`) when the web app needs to hide superuser-only pages. For ticket 05: `admin_user_audit_events.source` exists, but `ChangeUserRole` always writes `manual`; add a source-aware path (and let it skip the actor rules, with the person as actor) for group mapping.
