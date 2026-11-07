# 03: Stored IGDB secret and Apprise key

**What to build:** The IGDB client secret and the Apprise config key can be entered, stored and checked. A stored Apprise key satisfies the URL-plus-key pairing rule while the URL stays in env.

**Blocked by:** 01

**Status:** done

- [x] Both secrets can be set, replaced, cleared and checked (registry, checks, Playwright; no live check against Twitch/Apprise)
- [x] Validate accepts env URL plus stored Apprise key (unit test of resolution then Validate)
- [x] Apprise worker starts after restart using the stored key (by construction: app.go builds the worker from the resolved config; no live boot)
