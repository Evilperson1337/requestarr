# 05: Viewer demotion and last-admin guard

**What to build:** An admin can set a user back to viewer. The last admin cannot be demoted, so the portal cannot lock itself out.

**Blocked by:** 04 AGPL-3.0 license and real module path

**Status:** ready-for-agent

- [ ] The role endpoint accepts viewer, requester and admin
- [ ] Demoting the last admin returns a clear error and changes nothing
- [ ] The Settings UI can assign viewer
- [ ] HTTP-level and integration tests cover both rules
- [ ] The contract in api/openapi.yaml is updated first
