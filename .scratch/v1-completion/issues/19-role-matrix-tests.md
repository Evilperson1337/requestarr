# 19: Role matrix tests

**What to build:** Every mutating endpoint is tested against unauthenticated, viewer, requester and admin.

**Blocked by:** 05 Viewer demotion and last-admin guard, 08 Cancel and leave a Request

**Status:** ready-for-agent

- [ ] A table-driven test enumerates every mutating endpoint and role
- [ ] Unauthenticated requests are rejected; viewer cannot create; only admins reach admin endpoints
- [ ] Adding a new mutating endpoint without a row fails the test or is flagged
