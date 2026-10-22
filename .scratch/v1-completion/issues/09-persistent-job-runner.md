# 09: Persistent job runner

**What to build:** Syncs are recorded as job runs with status, times and errors; two syncs of the same Source never overlap, even across a restart. Jellyfin sync runs through it (D018).

**Blocked by:** 07 Jellyfin evidence moves Requests to available

**Status:** done

- [x] A new append-only migration adds the run table
- [x] A run records start, finish, outcome and error
- [x] The no-overlap guard holds across a process restart
- [x] A status endpoint lists recent runs per Source
- [x] Jellyfin sync is scheduled through the runner and the ad-hoc goroutine is gone
- [x] Integration tests cover overlap, crash recovery and failed-run recording; failed runs retry on the next tick
