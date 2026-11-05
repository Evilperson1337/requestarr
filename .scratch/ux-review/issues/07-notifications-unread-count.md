# 07: Requesters see how many notifications are unread

**What to build:** The Notifications nav item shows how many are unread, so a requester notices that their request was approved or became available without opening the page. (UX-005.)

**Blocked by:** 03 (both change the app's navigation)

**Status:** ready-for-agent

- [ ] The nav item shows the signed-in person's unread count when above zero, with an accessible name ("N unread"); it never counts anyone else's notifications
- [ ] Marking a notification read, or opening the page and reading them, updates the count without a reload; a new notification is reflected after the next navigation or refresh (no polling loop is added)
- [ ] An efficient count is available from the API (not a full list fetch for every page change); the API and OpenAPI describe it; the contract test passes
- [ ] Tests cover: count for the right user, zero hides the badge, mark-as-read updates it, a viewer and an admin both see their own count; Playwright with axe
- [ ] `scripts/check.sh` passes
