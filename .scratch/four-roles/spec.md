Status: ready-for-agent

# Four roles, and OIDC groups that decide them

Source: the conversation of 2026-11-06 after the rc.4 test on Unraid, `GLOSSARY.md` (Viewer, Requester, Local administrator), and D035 and D040 in `docs/DECISIONS.md`. It changes D035 (no identity is ever made an administrator automatically) and D040 (the local administrator is always an administrator).

## Problem Statement

After connecting OIDC, the operator signs in with their own account and finds they are a **viewer**: no Settings, no way to request anything. There is no way to tell Requestarr who is who from the identity provider, so every person starts at the bottom and an existing administrator has to promote them by hand, one by one. The operator had no OIDC scopes setting and no mapping, so nothing in the provider's groups could help.

The role model is also too coarse. There is one **admin** role that does everything: approve requests and also change how people sign in and where the credentials for every integration live. A household or a small team wants a day-to-day manager who can run requests without being able to change authentication or touch keys, and a single owner who can do everything.

## Solution

Four roles, in this order of power:

- **Superuser** (the owner): full control of every part of Requestarr. Nothing is withheld from this role.
- **Admin**: runs the system day to day: approves, denies and cancels requests, makes requests on behalf of people, resolves issues, reviews matches and notifications, and manages who is a requester or a viewer. Cannot change authentication, the local administrator, integration credentials or keys, or create and remove admins and superusers.
- **Requester** (shown as the user group): browses the libraries and makes requests. Sees no Settings.
- **Viewer**: browses libraries, requests and issues and can report an issue, but cannot request anything.

In Settings, Authentication the operator can choose the OIDC scopes and switch on **Assign roles from groups**. With it on, the groups in the person's ID token decide their role at every sign-in. The defaults are `owner` for superuser, `admin` for admin, `user` for requester and `viewer` for viewer, in a claim named `groups`, and each can be changed. The built-in local administrator becomes a superuser, and every existing administrator becomes a superuser on upgrade so nobody loses access.

## User Stories

### Roles
1. As an owner, I want a superuser role with no limits, so that nothing in Requestarr is ever out of my reach.
2. As an owner, I want an admin role for the person who runs things day to day, so that I can share the work without sharing the keys.
3. As an admin, I want to approve, deny and cancel requests, so that I can run the queue.
4. As an admin, I want to make a request on behalf of a person, so that I can help someone who cannot or will not use the site.
5. As an admin, I want to resolve and close issues, so that library problems get dealt with.
6. As an admin, I want to review and fix matches, so that items line up with the right metadata.
7. As an admin, I want to see the operational status and recent errors, so that I can tell when something has stopped syncing.
8. As an admin, I want to set people to requester or viewer, so that I can decide who may request.
9. As an admin, I do not want to be able to create or remove admins and superusers, so that the owner's control cannot be taken by someone I promoted.
10. As an admin, I want the Settings page to show only what I may use, so that I am not offered controls that answer "forbidden".
11. As a requester, I want to browse the libraries and make requests, so that I can ask for what I want.
12. As a requester, I want no Settings entry at all, so that the interface is not cluttered with things I cannot use.
13. As a viewer, I want to browse libraries, requests and issues, so that I can see what exists and what is wanted.
14. As a viewer, I want to report an issue with a library item, so that I can help even without being able to request.
15. As a viewer, I want a plain message saying my role cannot create requests, so that I know whom to ask.
16. As a superuser, I want to be the only one who can change how people sign in, so that nobody can lock the owner out.
17. As a superuser, I want to be the only one who can change integration credentials, keys and connections, so that secrets stay with the owner.
18. As a superuser, I want to be the only one who can manage the local administrator, so that the recovery path stays with the owner.
19. As a superuser, I want to promote anyone to any role, including admin and superuser, so that I can hand over or share ownership.
20. As a superuser, I want the instance to always have at least one superuser who can sign in, so that I can never remove the last way to manage it.
21. As the local administrator, I want to hold the superuser role, so that my recovery account can set up OIDC and everything else.
22. As an operator, I want the local administrator's role to stay fixed, so that it cannot be demoted by mistake.
23. As a person whose role changed, I want my other sessions ended, so that the new role takes effect straight away.

### Upgrade
24. As an existing administrator, I want to become a superuser when this ships, so that I lose no access.
25. As an existing requester or viewer, I want my role unchanged, so that nothing surprises me.
26. As an owner on an upgraded instance, I want to demote people who should be admins rather than superusers, so that I can adopt the new split at my own pace.
27. As an operator, I want the role history in the audit trail to keep reading correctly after the change, so that old entries still make sense.

### OIDC group mapping
28. As an operator, I want to switch on "Assign roles from groups" in Settings, Authentication, so that my identity provider decides who is what.
29. As an operator, I want the switch off until I turn it on, so that an upgrade never promotes people because of group names I did not know my provider sends.
30. As an operator, I want a group claim name with `groups` as the default, so that common providers work without typing.
31. As an operator, I want four group names with `owner`, `admin`, `user` and `viewer` as defaults, so that I can create matching groups and be done.
32. As an operator, I want to change any of the four names, so that I can match groups my organisation already has.
33. As an operator, I want to choose the OIDC scopes requested at sign-in, so that I can ask the provider to include groups.
34. As an operator, I want new setups to request the `groups` scope by default and existing setups to keep their current scopes, so that an upgrade never breaks a working sign-in.
35. As an operator, I want `openid` always requested whatever I type, so that I cannot break sign-in with an empty scope list.
36. As an operator, I want the Test button to say if the provider does not list the groups scope, so that I find out before people sign in.
37. As an operator, I want the Test button to still pass when the scope is unlisted, so that a provider that does not advertise its scopes can be used.
38. As a person in a mapped group, I want my role set at every sign-in, so that moving me between groups in the provider changes my role here.
39. As a person in several mapped groups, I want the most powerful one to apply, so that adding me to a second group never reduces my access.
40. As a person in no mapped group, I want to keep my current role, and a first-time person to get the default for new people, so that a missing group never locks anyone out.
41. As an operator, I want group names matched without regard to letter case, and a single leading slash ignored, so that a provider's path-style groups work.
42. As an operator, I want a claim that is a single text value to count as one group, so that providers that send a string work.
43. As an operator, I want the default role for new people to stay viewer or requester only, so that nobody becomes admin without an explicit mapping or a superuser.
44. As an operator, I want a group mapping that would demote the last superuser to be ignored, with a log line saying why, so that mapping can never leave the instance without an owner.
45. As a superuser, I want every role change made by group mapping recorded with its source, so that I can see why someone's role changed.
46. As a superuser, I want to see that a person's role is set by their groups, so that I understand why a manual change is overridden at their next sign-in.
47. As an operator, I want group mapping settings applied at once without a restart, so that I can fix a typo and try again.
48. As an operator, I want the group settings read-only when the environment configures OIDC, so that one source of truth wins, as with the other OIDC settings.
49. As an operator, I want group names and the claim shown, but never any person's group list stored, so that the identity provider remains the only record of groups.

### Settings and interface
50. As an admin, I want Settings to show Activity, Matches and Users but not Integrations or Authentication, so that I see only what I can use.
51. As a superuser, I want Settings to show everything, so that I can reach every control.
52. As a superuser, I want the Get started checklist, so that I can finish setup; admins do not see it.
53. As an admin, I want the people list to show who is a superuser or an admin but not let me change them, so that I understand the limit.
54. As a person using the app, I want every control hidden or explained by what my role can do, decided by one server answer, so that the interface and the server never disagree.
55. As an admin, I want the first-sign-in notice for new people, so that I can set their role, and I want it to say which role they were given.
56. As a person who is refused an action, I want a message that names the role needed, so that I know what to ask for.

### Requests on behalf
57. As an admin, I want to pick a person when I make a request, so that the request is theirs.
58. As the person a request was made for, I want to be its requester and be notified of its progress, so that it works as if I had made it.
59. As an admin, I want the request to say it was made on my behalf, so that the history is honest.
60. As a person, I do not want a second request for something that already has one, so that the one-shared-request rule still holds; I am subscribed to it instead.

## Implementation Decisions

- **Role model.** Stored roles are `viewer`, `requester`, `admin` and `superuser`, ranked in that order. One domain module defines the ranking and what each role may do, and every other module asks it. The scattered string comparisons against "admin" in the HTTP layer, the request, issue and notification services and the web app are replaced by questions to that module (or, in the web app, by capabilities the server returns).
- **Access levels.** The route table gains a **superuser** access level beside public, session and admin. The admin level admits admin and superuser. The role-policy test reads the table, so every route's level is checked against all four roles.
- **Superuser-only.** Settings, Authentication (state, save, test, enable, discard); local administrator management; the stored-secret endpoints; integration and provider connection settings and their credentials; the notification gateway key; and changing anyone's role to or from admin or superuser. The implementer enumerates the exact routes from the route table; the role matrix test is the authority and must list each.
- **Admin.** Everything else that is administrative today: request approval, denial, cancel and the awaiting count; issues; matches; operational status and activity (read only); approval policy; the default role for new people; changing people between requester and viewer; the Users list; the first-sign-in notices.
- **Schema.** A migration widens the role check on users and on the role-change audit to include `superuser`, and sets every existing admin (including the built-in local administrator) to `superuser`. Migrations stay append-only with up and down files. The role-change audit gains a source (manual, group mapping), and a group-mapping change records the person themselves as the actor.
- **Guards.** The last-administrator guard becomes a last-superuser guard, counting superusers who can sign in (the local administrator counts only while enabled). Nobody changes their own role. The built-in account's role is fixed at superuser. An admin may change a person only between requester and viewer, and may not touch an admin or superuser.
- **First sign-in.** The default role for new people stays viewer or requester (D035). Every admin and superuser receives the first-sign-in notice, and it names the role granted.
- **Session answer.** The session endpoint returns the role and the capabilities derived from it (request, manage requests, manage system). The web app decides what to show from the capabilities and never compares role names.
- **Group mapping settings.** Stored with the OIDC settings: the switch (default off), the group claim name (default `groups`), the four group names (defaults `owner`, `admin`, `user`, `viewer`) and the requested scopes. They are not secrets. They apply at once through the same atomic provider snapshot as the other OIDC settings, and are read-only when the environment configures OIDC, following the existing environment-as-a-unit rule.
- **Scopes.** The scope list is a saved setting. `openid` is always requested. New setups default to `openid profile email groups`. A migration stores `openid profile email` for an existing OIDC configuration so an upgrade changes nothing about the sign-in request. The Test button reads the provider's advertised scopes and adds a warning (not a failure) when `groups` is requested but not listed.
- **Reading groups.** Groups are read from the ID token only, by the claim name set. The claim may be a list of text values or a single text value. Matching ignores letter case and one leading slash. The userinfo endpoint is not called.
- **Applying groups.** At every sign-in, if the switch is on and at least one of the person's groups matches a mapped name, the highest-ranked matching role is applied, whether it raises or lowers the current role. If nothing matches, an existing person keeps their role and a new person gets the default for new people. Changing a role by group ends the person's other sessions and writes the role-change audit. A mapping that would leave no superuser who can sign in is skipped, and logged at warning level without naming the person's groups.
- **Privacy.** The person's group list is used for the sign-in decision and is not stored or logged.
- **Local administrator.** Always `superuser`. D040's statement that it is always an administrator is superseded.
- **Authentication page.** The OIDC card gains scopes, "Assign roles from groups", the claim name and four group fields with the defaults filled in, with the usual saved, test and read-only behaviour. The Users table shows when a role is applied by groups, and says a manual change is overridden at the person's next sign-in while mapping is on. Per the design review of the page, it keeps the Settings integration-card pattern.
- **Settings page.** An admin sees Activity, Matches and Users; Integrations and Authentication are superuser-only and the Get started checklist and its navigation entry are superuser-only. Controls the person cannot use are absent, not disabled.
- **Requests on behalf.** An admin or superuser may create a request for a chosen person. The person becomes the requester (subscription source requester) and is notified; the request records who made it on their behalf. The one-shared-request-per-item rule holds: if the item already has a request, the person is subscribed instead. The approval policy applies to the person as if they had asked. The request modal gains a person picker for those two roles only.
- **Decisions and docs.** A new decision records the four roles, the superuser-only list, the group mapping rules and the upgrade. It amends D035 (admin and superuser can be assigned by an explicit group mapping or by a superuser, never by default) and D040 (the local administrator is a superuser). The glossary gains Superuser and Admin, and updates the local administrator entry. `api/openapi.yaml` carries the role enums, the capabilities and the new settings in the same change as the routes. The Unraid and sign-in guides explain the group setup for Keycloak.
- **Vocabulary.** The user-facing label for the requester role stays "Requester"; the group default is `user`. Messages that say "administrator role required" name the actual role needed.

## Testing Decisions

- A good test drives external behaviour only: who can call which route, what a sign-in does to a role, what the page shows for a given server answer, what the database enforces. It does not look at private helpers.
- **Seam 1, the HTTP route policy.** The role matrix test is extended to four roles and the superuser level, so each route is asserted for viewer, requester, admin and superuser, with the 401 and 403 cases. Prior art: the existing role matrix and route-policy tests.
- **Seam 2, the OIDC sign-in callback.** With a fake provider that returns chosen group claims: mapping off and on, default names and custom names, a list and a single value, case and slash handling, several matching groups, no match for a new and an existing person, the last-superuser skip, session ending and the audit source. Prior art: the existing OIDC callback and local sign-in handler tests with fake stores.
- **Seam 3, Settings in the browser.** Playwright with deterministic HTTP responses, with axe at desktop and phone widths: the new OIDC card fields, the Test warning, read-only under environment settings, the Users table for admin and superuser, and the Settings tabs per role. Prior art: the authentication settings spec.
- **Seam 4, PostgreSQL.** Integration tests for the migration (existing admins become superusers, the built-in account included, other roles unchanged), the widened checks, the last-superuser guard under concurrent changes, and the stored group-mapping settings. Prior art: the local administrator integration tests and the role-change audit tests.
- A test asserts the person's group list never reaches the log, the database or an audit row, using a canary group name, as other secrets tests do.
- The upgrade is tested from a database holding the old roles, not from a fresh one.

## Out of Scope

- Custom roles or per-person permissions beyond the four roles.
- Reading groups from the userinfo endpoint, nested claim paths, or more than one claim.
- Demoting a person automatically because they left a group; only a different matching group or a manual change lowers a role.
- Several identity providers at once, SCIM or automatic provisioning.
- Changing the approval policy model, the one-shared-request rule or how viewers report issues.
- A per-route permission editor in the interface.

## Further Notes

- The switch is off by default on purpose: a provider that already sends groups named `admin` or `owner` would otherwise promote people at the next sign-in after an upgrade. The page should say that turning it on can change roles.
- Keycloak needs a client scope with a group membership mapper that adds the `groups` claim to the ID token; the guide should show it, and the Test warning exists because the claim cannot be checked without a sign-in.
- Requests on behalf is the largest new behaviour and is independent of the role split; it can ship as its own ticket after the roles.
- The Get started checklist and the OIDC-configured item stay as they are; only who sees them changes.
