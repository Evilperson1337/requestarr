# 05: Viewer demotion and last-admin guard

**What to build:** An admin can set a user back to viewer. The last admin cannot be demoted, so the portal cannot lock itself out.

**Blocked by:** 04 AGPL-3.0 license and real module path

**Status:** done

- [x] The role endpoint accepts viewer, requester and admin
- [x] Demoting the last admin returns a clear error and changes nothing
- [x] The Settings UI can assign viewer
- [x] HTTP-level and integration tests cover both rules
- [x] The contract in api/openapi.yaml is updated first
