# 02: New people get a sensible role, and admins are told they exist

**What to build:** An admin can choose what role a person gets the first time they sign in (`viewer`, the default, or `requester`), and every admin is told in-app when someone signs in for the first time, with a link to Settings, Users. This introduces the small settings store the next ticket reuses. (UX-003; D035.)

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [x] A settings store holds "default role for first sign-in", valid values `viewer` and `requester`, default `viewer`; it can never be `admin`; it applies only to users created after it is set
- [x] An admin can read and change it in Settings, Users (admin only); a requester or viewer cannot; the API and OpenAPI describe it; the contract test passes
- [x] The first sign-in of a new identity creates the user with the configured role and sends an in-app notification to every admin ("<name> signed in for the first time as <role>") that links to Settings, Users; a repeat sign-in sends nothing; the person who signs in is never notified about themselves
- [x] The viewer's "cannot create Requests" message tells them what to do next (ask an administrator) and names the role
- [x] Tests cover: default stays `viewer`; `requester` default applies to a new user and not to an existing one; `admin` is rejected; the notification goes once, to admins only; role change for existing users is unchanged
- [x] D035 is written in `docs/DECISIONS.md`
- [ ] Integration tests run against your own throwaway database, not `requestarr_test`; `scripts/check.sh` passes at the end
