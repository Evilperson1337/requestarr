# 02: Superuser exists, and existing admins and the local administrator become one

**What to build:** A `superuser` role is stored and works. On upgrade every existing admin, and the built-in local administrator, becomes a superuser, so nobody loses access. The Users page offers all four roles, with an admin limited to requester and viewer, and the instance always keeps a superuser who can sign in. (Spec stories 1, 19-27, 53, 55; Implementation Decisions: schema, guards, first sign-in.)

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] A new append-only migration (up and down) widens the role checks on users and on the role-change audit to include `superuser`, adds an audit source (manual or group mapping), and sets every existing admin, the built-in account included, to `superuser`; requesters and viewers are unchanged
- [ ] An integration test starts from a database holding the old roles and proves the upgrade, including the built-in account
- [ ] The built-in local administrator's role is fixed at superuser and cannot be changed or removed
- [ ] The last-administrator guard becomes a last-superuser guard, counting superusers who can sign in (the local administrator only while enabled), and holds under concurrent role changes (integration test)
- [ ] A superuser can set anyone to any of the four roles; an admin can change a person only between requester and viewer and cannot change an admin or a superuser; nobody can change their own role; each refusal has a clear message naming the role needed
- [ ] A role change still ends the person's other sessions and is audited with its source
- [ ] The Users table shows the four roles, labels superusers and admins, and offers only the choices the signed-in role may make, with Playwright and axe at desktop and phone widths
- [ ] The first-sign-in notice goes to every admin and superuser and names the role granted
- [ ] The OpenAPI contract, the role matrix test and the glossary entries for Superuser and Admin are updated; `scripts/check.sh` passes
