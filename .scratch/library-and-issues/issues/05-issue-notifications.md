# Issue notifications
Status: resolved
Blocked by: 03

New Issue: admins in-app plus the global Apprise outbox. Resolve/close/reopen/comment: the Issue's reporters. Reuse the existing durable notification and outbox paths.

Acceptance: tests that admins and only reporters are notified; muted (left) reporters are not.

## Answer

Built, with one change from the spec: reporters are notified **in-app only**. The Apprise route is a single operator-configured channel (per-user Apprise targets are deprecated, migration 10), so it hears about new Issues and status changes, never comments or joins. Notifications are written in the same transaction as the event.
