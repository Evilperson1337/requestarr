# 03: Stored IGDB secret and Apprise key

**What to build:** The IGDB client secret and the Apprise config key can be entered, stored and checked. A stored Apprise key satisfies the URL-plus-key pairing rule while the URL stays in env.

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] Both secrets can be set, replaced, cleared and checked
- [ ] Validate accepts env URL plus stored Apprise key
- [ ] Apprise worker starts after restart using the stored key
