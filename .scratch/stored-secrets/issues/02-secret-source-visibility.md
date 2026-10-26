# 02: Source visibility on Review

**What to build:** Each secret shows environment, stored, or stored (shadowed by environment). Review warns on shadowed values, lists secrets saved and pending restart, and shows stored secret unreadable. Stored values are never auto-deleted.

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] Source badge shown for every secret field
- [ ] Shadowed warning appears on Review and clears when the stored value is cleared
- [ ] Pending-restart list is accurate
- [ ] Frontend smoke test covers the states
