# 05: Groups decide roles at sign-in

**What to build:** With "Assign roles from groups" switched on, the groups in the person's ID token set their role at every sign-in, using the default names owner, admin, user and viewer unless the operator changes them. The switch is off by default. (Spec stories 28-32, 38-49, 46; Implementation Decisions: group mapping, reading and applying groups, privacy.)

**Blocked by:** 02, 04

**Status:** done

- [x] Settings, Authentication gains the switch (off by default), the group claim name (default `groups`) and four group names (defaults `owner`, `admin`, `user`, `viewer`); they apply at once, are not secrets, and are read-only when the environment configures OIDC
- [x] Groups are read from the ID token only, as a list or a single text value; matching ignores letter case and one leading slash
- [x] At each sign-in with the switch on and at least one matching group, the highest-ranked matching role is applied, raising or lowering the role; with no match an existing person keeps their role and a new person gets the default for new people (callback tests with a fake provider for each case)
- [x] A mapping that would leave no superuser who can sign in is skipped and logged at warning level without the person's groups
- [x] A role changed by group ends the person's other sessions and writes the role-change audit with the group-mapping source
- [x] A test with a canary group name proves the person's group list never reaches the log, the database or an audit row
- [x] The Users table shows when a person's role is set by groups and says a manual change is overridden at their next sign-in while mapping is on
- [x] The page says that turning mapping on can change roles; Playwright with axe at desktop and phone widths covers the fields, the warning and the read-only state
- [x] The OpenAPI contract is updated; `scripts/check.sh` passes

## Comments

- Branch `feat/four-roles-05`. No migration: the settings live in the existing `authentication.oidc` JSON, and `admin_user_audit_events.source` (`group_mapping`) already existed from migration 40.
- Criterion 1 (settings): flat fields on `authsettings.Settings`: `assignRolesFromGroups` (default false), `groupsClaim` (`groups`), `ownerGroup` (`owner`), `adminGroup` (`admin`), `userGroup` (`user`), `viewerGroup` (`viewer`); blank saves as the default, so a configuration saved before this ticket shows the defaults with mapping off. `internal/authsettings/groups_test.go` checks defaults, that a save is visible in `Service.Snapshot().Groups` at once (no restart, same atomic snapshot), that enabling keeps them, validation, and that the environment gives mapping off with the fields reported read-only. Playwright covers the fields and the read-only state (mocked HTTP, as spec seam 3 intends).
- Criterion 2 (reading groups): `internal/auth/oidc/groups_test.go` signs real ID tokens and runs the real client `Exchange`: list, single value, custom claim, missing claim, wrong type, non-text items. `domain/group_mapping_test.go` covers case, one leading slash (two slashes or a nested path do not match) and blank names.
- Criterion 3 (callback): `internal/auth/group_roles_test.go` drives the real login and callback handlers with a fake provider returning chosen groups: raise, lower, highest wins, already equal (no call), no match, no groups, switch off, case/slash, custom names. A new person getting the default is the existing `Login` path; mapping only applies when a group matches.
- Criterion 4 (last superuser): `Store.ApplyGroupRole` returns `auth.ErrLastSuperuser` (the postgres name is an alias); the handler logs a fixed warning without groups and still signs the person in with their role (handler test asserts the canary group is not in the log; PostgreSQL test `TestApplyGroupRoleNeverRemovesTheLastSuperuserIntegration`).
- Criterion 5 (sessions, audit): `TestApplyGroupRoleChangesTheRoleEndsSessionsAndAuditsItIntegration` (role, all sessions revoked, audit row with source `group_mapping` and the person as actor, no second row for an unchanged role). The role is applied before the new session is issued, so the person's fresh session carries the new role. The built-in account is never changed by groups.
- Criterion 6 (canary): `TestGroupNamesNeverReachTheDatabaseOrTheLogIntegration` signs in through the real handlers and real PostgreSQL with a canary group, then scans every public table (`row::text`) and the log. Structurally, the store method takes only the role, never the groups.
- Criterion 7 (Users table): `GET /admin/users` returns `rolesFromGroups` (mapping is on and OIDC working) and per person `roleFromGroups` (their latest audit row has source `group_mapping`; a later manual change clears it, tested in PostgreSQL). The page shows a "Set by groups" badge and a note that a manual change is overridden at the next sign-in; Playwright (mocked HTTP) at 1440 and 390 with axe, admin and superuser.
- Criterion 8: the card carries the "can change roles" warning; Playwright with axe at 1440 and 390 covers fields, save, and the read-only state. `api/openapi.yaml` updated (settings, update, users list); the OpenAPI schema tests pass. `scripts/check.sh` result below.
- `scripts/check.sh` passed (fmt, tests, vet, PostgreSQL integration on requestarr_t05, tsc, 350 Playwright tests).
- Known limit: a first-time person is created with the default role, then raised or lowered by groups in the same sign-in, so the first-sign-in notice names the default role and the audit shows the group change right after.
- For ticket 07: group names match ignoring case and one leading slash; the most powerful matching group wins; no match keeps the role (new people get the default for new people); the switch is off by default and under environment configuration mapping is off; the claim may be a list or one text value; Keycloak needs a group membership mapper on a client scope that adds `groups` to the ID token.
