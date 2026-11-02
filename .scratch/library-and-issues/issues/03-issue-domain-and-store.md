# Issue domain, kinds and store

Status: resolved
Blocked by:

- `domain.Issue`, `IssueKind` lists per media type, part via `Selection` (validated against the item's media type), statuses open/resolved/closed.
- Migration: `issues`, `issue_reporters` (note per reporter), `issue_events`.
- Report merges into the open Issue for the same (Library item, kind, part); admin resolve/close/reopen with comment; reporter leaves.
- Per-user report rate limit.

Acceptance: unit tests for kind validation and merge; integration tests including 'item leaves library keeps Issue open and labelled'.

## Answer

Built: `domain.Issue`, kinds per media type, `internal/issues` service, `issues` / `issue_reporters` / `issue_events` (migration 28), hourly limit of 10, shared open Issue per item/kind/part, admin-only transitions, "gone from library" flag. HTTP is ticket 04.
