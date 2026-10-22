# 14: Jellyfin onto the adapter interface

**What to build:** Jellyfin uses the same Source interface and contract suite as every other Source, with no change in behaviour.

**Blocked by:** 11 Adapter interface, contract suite, multi-source availability and Plex movie/TV

**Status:** done

- [x] Jellyfin passes the shared contract suite using fixtures
- [x] Its source identity is provided to the adapter at construction and is reflected in its descriptor and snapshots
- [x] Existing availability and absence behaviour is unchanged (integration tests pass)
