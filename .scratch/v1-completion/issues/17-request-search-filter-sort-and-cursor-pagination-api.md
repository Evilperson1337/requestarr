# 17: Request search, filter, sort and cursor pagination (API)

**What to build:** The API supports title search, status and media-type filters, sort, and cursor pagination. The contract in api/openapi.yaml is updated first.

**Blocked by:** 04 AGPL-3.0 license and real module path, 06 Consistent error shape and request ID

**Status:** done

- [x] The OpenAPI contract is updated before the handler
- [x] Search, filters and sort work and combine with the mine filter
- [x] Cursor pagination is stable while Requests change
- [x] Invalid parameters return the standard error shape
- [x] HTTP and integration tests cover the combinations

**Delivered:** Contract-first literal case-insensitive title search, single
status/media-type filters, subscription-based My Requests, newest/oldest/status
sort, and opaque signed cursor pagination. All orders use ascending ID ties;
status order is requested, approved, available, denied, cancelled, then newest.
Default/max page size remains 100, with supported sizes 1–100.

**Confirmed pagination decision:** Freeze matching IDs/order for 15 minutes,
but return current request details. Persist snapshots and signing keys in
PostgreSQL; at most 10,000 IDs and five retained paginated views per user.
A sixth view evicts the oldest. Oversized results fail explicitly rather than
truncate. Cursors bind user and normalized query/page size. Refresh without a
cursor to include new/moved requests. Browser controls are ticket #18 scope.
See `docs/REQUESTS_API.md` for the public contract and limits.

**Validation:** `make fmt`, `make test`, `make vet`, `make web-check`, `make build`,
`make test-integration REQUESTARR_POSTGRES_PORT=5433`, focused race-enabled
PostgreSQL/HTTP listing tests, API YAML parsing, and `git diff --check` passed.
Tests cover combined filters, every status/media type, literal wildcard/SQL
characters, tie ordering, live mutations during paging, replay/tampering,
cross-user/query isolation, expiry/eviction, concurrent retention, and the
10,000-ID boundary. Independent Standards and Spec reviews reported no findings.
