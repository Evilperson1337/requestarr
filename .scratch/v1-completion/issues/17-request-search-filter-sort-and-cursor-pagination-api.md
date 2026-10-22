# 17: Request search, filter, sort and cursor pagination (API)

**What to build:** The API supports title search, status and media-type filters, sort, and cursor pagination. The contract in api/openapi.yaml is updated first.

**Blocked by:** 04 AGPL-3.0 license and real module path, 06 Consistent error shape and request ID

**Status:** ready-for-agent

- [ ] The OpenAPI contract is updated before the handler
- [ ] Search, filters and sort work and combine with the mine filter
- [ ] Cursor pagination is stable while Requests change
- [ ] Invalid parameters return the standard error shape
- [ ] HTTP and integration tests cover the combinations
