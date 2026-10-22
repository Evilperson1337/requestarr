# 08: Cancel and leave a Request

**What to build:** A requester can leave a shared Request. It closes only when the last Subscriber leaves. An admin can cancel any Request outright (D016).

**Blocked by:** 04 AGPL-3.0 license and real module path

**Status:** done

- [x] Leaving removes the user's subscription and keeps the Request open when other Subscribers remain
- [x] Leaving as the last Subscriber moves the Request to cancelled with an audit event
- [x] Admins can cancel outright from the UI and API
- [x] Optimistic-concurrency and idempotency-key behaviour still hold
- [x] Unit, HTTP and integration tests cover each case; the UI shows the actions to the right roles
