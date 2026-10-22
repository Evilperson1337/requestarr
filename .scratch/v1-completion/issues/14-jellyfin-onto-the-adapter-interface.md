# 14: Jellyfin onto the adapter interface

**What to build:** Jellyfin uses the same Source interface and contract suite as every other Source, with no change in behaviour.

**Blocked by:** 11 Adapter interface, contract suite, multi-source availability and Plex movie/TV

**Status:** ready-for-agent

- [ ] Jellyfin passes the shared contract suite using fixtures
- [ ] Its source identity is configuration, not hard-coded
- [ ] Existing availability and absence behaviour is unchanged (integration tests still pass)
