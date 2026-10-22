# 11: Adapter interface, contract suite, multi-source availability and Plex movie/TV

**What to build:** A single narrow Source interface, a shared contract test suite, the multi-Source availability rule, and Plex movie and TV as the first adapter through it (D015, D019).

**Blocked by:** 09 Persistent job runner

**Status:** done

- [x] The interface yields a complete normalized snapshot for the Source's configured media types, or an error distinct from empty
- [x] Each Source declares its media types and identifier kinds
- [x] An item in two Sources stays available until every covering Source has completed a sync without it
- [x] The contract suite runs over recorded fixtures with success and failure cases
- [x] Plex movie/TV passes the contract suite and drives availability through the job runner
- [x] A failed Plex sync never removes anything
