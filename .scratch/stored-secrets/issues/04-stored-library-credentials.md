# 04: Stored Jellyfin, Plex and RomM credentials

**What to build:** Each library credential can be entered, stored and checked like the TMDB token. A stored credential satisfies the URL-plus-secret rule with the URL in env. Library probes keep their existing safety limits (redirects off, pinned DNS, size and time limits).

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] Each of the three can be set, replaced, cleared and checked
- [ ] A stored-only credential makes the Source configured after restart
- [ ] Existing probe safety tests still pass
