# 03: State-aware request actions and confirmation

**What to build:** A user sees the correct next action for a movie's local shared-Request and availability state, can add an optional note and submit safely, and receives an explicit in-modal success confirmation instead of being navigated away immediately. Deliver local state lookup, authenticated response/action contracts, and the full interactive movie flow together.

**Blocked by:** 01 — Rich movie confirmation preview. Not blocked by 02 — Director, cast, and backdrop enrichment.

**Status:** done

## Acceptance criteria

- [x] Supply local shared-Request identity/lifecycle, current-user subscription, role-appropriate permissions, and authoritative library-availability information needed by the preview without live provider/library calls to render it.
- [x] A new movie not known available offers Request movie to an authorized requester/admin. The sticky footer keeps the primary action and Cancel/Close reachable while details scroll.
- [x] Add a note expands the existing optional note input rather than displaying a large empty textarea by default. Preserve the existing limit and retain the note across recoverable submission failures.
- [x] An already requested movie shows the shared Request state, Notify Me or Notifications On as appropriate, and View request instead of duplicate creation. Preserve closed-Request subscription rules and use actual subscription results.
- [x] Known available content shows Source-authoritative availability and Open in library when a usable link exists. Do not invent links, perform live library scans, infer availability from TMDB release dates, or present unknown availability as missing. Existing Request state and known availability must not contradict each other.
- [x] View request leads to the correct shared Request through supported shared-Requests navigation; include minimal navigation support if necessary rather than using an ambiguous title search or adding a standalone movie-details destination.
- [x] Upcoming movies remain requestable when the user/policy permits. Viewers do not receive creation controls they cannot use, and all backend authentication, role, CSRF, and Origin controls remain authoritative.
- [x] Pending submission is announced and prevents duplicate in-flight clicks. Canonical shared-Request idempotency handles retries and another user's concurrent creation without a second Request.
- [x] A recoverable failure keeps the preview and note intact with a safe retryable error. Authorization loss clears or disables protected actionable state rather than permitting stale submission.
- [x] Success leaves the modal open, removes Request movie, confirms the actual shared Request and notification subscription, and offers View request and Close. A race that returns an existing Request must not claim a new one was created or notifications enabled without confirmation.
- [x] Sticky controls, the note input, and the phone keyboard do not obscure each other. Preserve focus containment/return, close/Escape behavior, and announced pending/error/success states.
- [x] API regressions use real services and deterministic repositories/provider fakes to verify new/existing/available/unknown states, allowed/denied roles, subscription changes, closed Requests, concurrent creation, retry, and no raw-error/credential leakage. Add durable-store contract tests where state semantics change.
- [x] Browser regressions cover state-appropriate actions, note expansion/preservation, pending submission, retry, in-modal confirmation, actual View request navigation, phone layout, keyboard behavior, and authorization loss.
- [x] Update the movie smoke-flow expectation from automatic navigation to in-modal confirmation while preserving TV, music, and game behavior. Update affected API/user guidance and pass required formatting, full Go tests, vet, and frontend checks.

## Independence and scope

This slice must work with Ticket 1's core preview even if Ticket 2 is not yet
complete. It does not change request policy, create per-user duplicates, add
voting, replace OIDC, or introduce acquisition functionality. No destructive
state reset, credential rotation, commit, or stack restart is part of this ticket.

## Completion and validation

Implemented an explicit required local-preview reader, authenticated no-store local-state endpoint, role-appropriate action flags, safe library links, exact Request-ID queue filtering, expandable notes, retry preservation, in-flight submission guard, actual subscription state, concurrent-existing-Request handling, and in-modal success. Added role/anonymous/invalid-input tests, browser state/retry/authorization/concurrency/focus regressions, and real PostgreSQL persistence/navigation/link tests. Encoded credential query keys are rejected at the library-link boundary.

Validation passed: required formatting, full Go tests, vet, frontend typechecks,
production frontend build, all 165 browser tests, targeted race tests, and
targeted PostgreSQL tests against a disposable database. Review regressions
also caught and fixed link contrast, non-movie focus return, null credits,
near-deadline enrichment, and encoded credential-bearing library links.

Implementation is not deployed. The running stack was not rebuilt or restarted;
no application database, credentials, encryption key, realm, or commits were
changed. Test databases were removed. Existing unrelated work was preserved.
