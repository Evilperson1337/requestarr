# 02: Source visibility on Review

**What to build:** Each secret shows environment, stored, or stored (shadowed by environment). Review warns on shadowed values, lists secrets saved and pending restart, and shows stored secret unreadable. Stored values are never auto-deleted.

**Blocked by:** 01

**Status:** ready-for-agent

- [x] Source badge shown for every secret field
- [x] Shadowed warning appears on Review and clears when the stored value is cleared
- [x] Pending-restart list is accurate
- [x] Frontend smoke test covers the states
