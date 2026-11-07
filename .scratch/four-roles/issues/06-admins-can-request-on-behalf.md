# 06: Admins can request on behalf of a person

**What to build:** An admin or superuser can pick a person when making a request, and the request becomes that person's: they are its requester and are notified of its progress. (Spec stories 4, 57-60; Implementation Decisions: requests on behalf.)

**Blocked by:** 01

**Status:** done

- [x] The request modal shows a person picker to admins and superusers only; requesters and viewers never see it, and the server refuses the field from them
- [x] The chosen person becomes the requester (subscription source requester) and receives the usual notifications; the request records who made it on their behalf and the request detail says so
- [x] When the item already has a request the person is subscribed to it instead of a second one being created (one shared request per item)
- [x] The approval policy applies to the person as if they had asked
- [x] Handler and role tests cover each role; Playwright with axe at desktop and phone widths covers the picker
- [x] The OpenAPI contract is updated; `scripts/check.sh` passes

## Comments

Checked by running real code:

- Picker for admins only: `RequesterPicker` renders only when `session.capabilities.manageRequests`, and fetches the people list only then. Playwright (`web/tests/request-on-behalf.spec.ts`) covers admin (offers "Myself" plus people who `canRequest`), requester and viewer (no picker, no `/admin/users` call). The server refuses the field: `TestCreateRequestOnBehalfIsRefusedForRolesThatCannotManageRequests` (viewer, requester give 403, nothing created); the service asks `Role.CanManageRequests()`, so superuser is admitted once ticket 02 stores it (`TestOnBehalfWorksForSuperuser` uses the role module).
- Requester and notices: PostgreSQL integration test `TestRequestMadeOnBehalfIsThePersonsAndRecordsTheAdministrator` shows `requested_by` is the person, subscription source `requester`, the administrator is not subscribed, and the person got in-app notices. Migration `000041` adds `requests.made_on_behalf_by`; the `request.created` event's actor is the administrator with `onBehalfOf` in its data. `GET /requests` returns `madeOnBehalfBy` and `madeOnBehalfByName`, and the requests page prints "Made on behalf of the requester by NAME" (Playwright).
- One shared request: the same test makes a second on-behalf request for the item: still one row, the second person subscribed with source `manual`.
- Approval policy: service test and the integration test (auto-approve movies gives an approved request for the person).
- Axe passes in the picker's modal at 1280 and 390 px wide.
- OpenAPI: `onBehalfOf`, the `madeOnBehalfBy*` response fields and `AdminUser.canRequest` documented.
- Not run by me: the superuser role itself (ticket 02); `web/tests/capabilities.ts` does not yet map superuser.
