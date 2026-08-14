# Prompt 04 — Shared Requests + Notify Me

Implement the shared request domain/API/UI. Enforce one request per canonical item transactionally. The creator is automatically subscribed. Existing-request resolution must expose Notify Me/Notifications On rather than duplicate creation. Implement shared Requests page filters including `user=me`, status/type/requester/sort, audit events, admin approve/deny, requester/admin notes, cancellation rules, and authorization. Add race-condition tests for simultaneous duplicate submissions.
