# 05: Groups decide roles at sign-in

**What to build:** With "Assign roles from groups" switched on, the groups in the person's ID token set their role at every sign-in, using the default names owner, admin, user and viewer unless the operator changes them. The switch is off by default. (Spec stories 28-32, 38-49, 46; Implementation Decisions: group mapping, reading and applying groups, privacy.)

**Blocked by:** 02, 04

**Status:** ready-for-agent

- [ ] Settings, Authentication gains the switch (off by default), the group claim name (default `groups`) and four group names (defaults `owner`, `admin`, `user`, `viewer`); they apply at once, are not secrets, and are read-only when the environment configures OIDC
- [ ] Groups are read from the ID token only, as a list or a single text value; matching ignores letter case and one leading slash
- [ ] At each sign-in with the switch on and at least one matching group, the highest-ranked matching role is applied, raising or lowering the role; with no match an existing person keeps their role and a new person gets the default for new people (callback tests with a fake provider for each case)
- [ ] A mapping that would leave no superuser who can sign in is skipped and logged at warning level without the person's groups
- [ ] A role changed by group ends the person's other sessions and writes the role-change audit with the group-mapping source
- [ ] A test with a canary group name proves the person's group list never reaches the log, the database or an audit row
- [ ] The Users table shows when a person's role is set by groups and says a manual change is overridden at their next sign-in while mapping is on
- [ ] The page says that turning mapping on can change roles; Playwright with axe at desktop and phone widths covers the fields, the warning and the read-only state
- [ ] The OpenAPI contract is updated; `scripts/check.sh` passes
