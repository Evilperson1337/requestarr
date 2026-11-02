# Issue API

Status: resolved
Blocked by: 03

`POST /issues`, `GET /issues` (admin: all, others: mine, filters status/kind/type, snapshot pagination), `GET /issues/{id}`, comment, resolve/close/reopen (admin), leave. OpenAPI, role matrix rows (every role may report; only admin transitions).

Acceptance: role-matrix and OpenAPI contract tests green.

## Answer

Built: POST /issues (201 new, 200 joined), GET /issues (filters, keyset paging; admin all, others own), GET /issues/summary, GET /issues/{id} (timeline; reporters per role), POST /issues/{id}/comments, POST /issues/{id}/status (admin), DELETE /issues/{id}/reporter. OpenAPI, role matrix rows and handler tests included.
