# 16: Suggested matches review

**What to build:** Inexact matches are shown to admins as suggestions to accept or dismiss. A suggestion never marks anything available on its own.

**Blocked by:** 11 Adapter interface, contract suite, multi-source availability and Plex movie/TV

**Status:** done

- [x] Title/year matches are recorded as suggestions, not evidence
- [x] An admin can accept or dismiss from the UI
- [x] Accepting records evidence and follows the normal availability path
- [x] Tests cover accept, dismiss, and that suggestions alone never change status

**Delivered:** Durable, audited title/year candidates with indexed conservative
matching, confidence/reasons, admin-only list/accept/dismiss API, and review UI
under Settings. Acceptance uses ordinary source/platform evidence and the existing
availability/event/notification transaction. Decisions are idempotent and
terminal; accepted matches survive resyncs and use normal absence safeguards.
Stale candidates cannot be accepted; failed syncs preserve the last-known view.

**Validation:** `make fmt`, `make test`, `make vet`, `make web-check`, `make build`,
and `make test-integration REQUESTARR_POSTGRES_PORT=5433` passed. PostgreSQL tests
cover no-evidence suggestions, accept/dismiss, conservative candidate rules,
stable-ID precedence, stale and failed syncs, absence reconciliation, and
concurrent reviews. Router tests cover admin/viewer/requester/unauthenticated
paths, CSRF, and safe errors. Independent Standards and Spec reviews completed;
both Standards findings were fixed and verified in follow-up review. Browser
interaction was not manually exercised; the UI was typechecked and built.
