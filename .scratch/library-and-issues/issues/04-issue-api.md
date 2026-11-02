# Issue API

Status: ready-for-agent
Blocked by: 03

`POST /issues`, `GET /issues` (admin: all, others: mine, filters status/kind/type, snapshot pagination), `GET /issues/{id}`, comment, resolve/close/reopen (admin), leave. OpenAPI, role matrix rows (every role may report; only admin transitions).

Acceptance: role-matrix and OpenAPI contract tests green.
