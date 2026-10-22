# 07: Jellyfin evidence moves Requests to available

**What to build:** When a Jellyfin sync finds an item that has open Requests, those Requests become available, subscribers get one in-app notification each, and the Apprise route gets one event. This is the first tracer through the whole availability path (D014, D015).

**Blocked by:** 02 Integration test target, 04 AGPL-3.0 license and real module path

**Status:** done

- [x] In the same transaction, the item's availability, open Requests' status and version, the audit event and the RequestAvailable event are written
- [x] Re-running the same sync produces no duplicate events or notifications
- [x] A failed or partial sync changes nothing and removes nothing
- [x] An integration test creates a Request, runs a snapshot, and asserts status, events and both notification channels
- [x] The unused reconcile package and its tautological test are removed or folded in, with nothing left importing the old path
- [x] The Requests page shows the Request as available
