# 06: Admins can request on behalf of a person

**What to build:** An admin or superuser can pick a person when making a request, and the request becomes that person's: they are its requester and are notified of its progress. (Spec stories 4, 57-60; Implementation Decisions: requests on behalf.)

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] The request modal shows a person picker to admins and superusers only; requesters and viewers never see it, and the server refuses the field from them
- [ ] The chosen person becomes the requester (subscription source requester) and receives the usual notifications; the request records who made it on their behalf and the request detail says so
- [ ] When the item already has a request the person is subscribed to it instead of a second one being created (one shared request per item)
- [ ] The approval policy applies to the person as if they had asked
- [ ] Handler and role tests cover each role; Playwright with axe at desktop and phone widths covers the picker
- [ ] The OpenAPI contract is updated; `scripts/check.sh` passes
