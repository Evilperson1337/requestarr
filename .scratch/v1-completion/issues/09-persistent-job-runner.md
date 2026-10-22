# 09: Persistent job runner

**What to build:** Syncs are recorded as job runs with status, times and errors; two syncs of the same Source never overlap, even across a restart. Jellyfin sync runs through it (D018).

**Blocked by:** 07 Jellyfin evidence moves Requests to available

**Status:** ready-for-agent

- [ ] A new append-only migration adds the run table
- [ ] A run records start, finish, outcome and error
- [ ] The no-overlap guard holds across a process restart
- [ ] A status endpoint lists recent runs per Source
- [ ] Jellyfin sync is scheduled through the runner and the ad-hoc goroutine is gone
- [ ] Integration tests cover overlap, crash recovery and failed-run recording; failed runs retry on the next tick
