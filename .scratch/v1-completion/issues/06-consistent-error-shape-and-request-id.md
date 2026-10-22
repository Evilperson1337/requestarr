# 06: Consistent error shape and request ID

**What to build:** Every API error uses one JSON shape and carries a request identifier, so clients and logs can be correlated.

**Blocked by:** 04 AGPL-3.0 license and real module path

**Status:** done

- [ ] All handlers return the same error shape; none use plain-text errors
- [ ] Each response includes a request ID that also appears in logs
- [ ] api/openapi.yaml documents the shape
- [ ] Tests assert the shape for common failure statuses
