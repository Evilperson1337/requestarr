Status: ready-for-agent

# V1 completion

Source: `docs/research/PROJECT_STATUS.md` (2026-10-21), the grilling session that followed it, `GLOSSARY.md`, and decisions D013–D019 in `docs/DECISIONS.md`.

## Problem Statement

Requestarr works as a prototype but is not releasable. A user can sign in, resolve a pasted link and file a shared Request, and Jellyfin movie/TV sync can mark an item available. But:

- A Request's status never becomes `available`, so people are never told their Request is done through the normal state, and the code that was meant to do this is unused.
- Only one of four Sources exists. Music and game availability cannot be established.
- Sync is an unmanaged background loop with no history, no manual trigger and no visibility.
- The Requests page cannot search, filter, sort, paginate or cancel.
- The first-run wizard is only an admin-claim form, and there is no settings or operational status view.
- A viewer cannot be set back to viewer, and the last admin is not protected.
- There is no license, the module path is a placeholder, CI never runs on pushes, the default test run never exercises SQL, and the docs describe a scaffold that no longer exists.

## Solution

Finish V1 as scoped in `docs/V1_SCOPE.md`, in this order (D019): baseline hygiene; the `available` flow; the persistent job runner; the Source adapter interface with Plex, RomM, Jellyfin (moved onto it) and Navidrome; the Requests page; the setup wizard with settings and operational status; release hardening.

From a user's view: they file one shared Request per item, are told when any configured library has it, can find and manage Requests easily, and admins can configure, monitor and trigger syncs from the UI.

## User Stories

### Requesting and the shared queue
1. As a requester, I want to paste a movie, TV, music or game link and see what it resolves to, so that I confirm it is the right item before requesting.
2. As a requester, I want only one Request to exist per item, so that my ask joins the existing one rather than duplicating it.
3. As a requester, I want to be subscribed automatically to a Request I create, so that I hear when it is done.
4. As a user, I want to press Notify Me on someone else's Request, so that I hear about it without creating a duplicate.
5. As a subscriber, I want to unsubscribe, so that I stop hearing about it.
6. As a requester, I want to cancel my Request, so that I can withdraw an ask I no longer want.
7. As a requester, I want my cancellation to leave the Request open when other Subscribers remain, so that I do not take away what others want.
8. As a requester, I want the Request to become `cancelled` when I was the last Subscriber, so that dead asks leave the queue.
9. As an admin, I want to cancel any Request outright, so that I can clean up the queue.
10. As a user, I want to search the Requests page by title, so that I can check whether something was already requested.
11. As a user, I want to filter by status and media type, so that I can narrow a large queue.
12. As a user, I want to sort by newest, oldest and status, so that I see what matters first.
13. As a user, I want paging that stays stable while Requests change, so that I do not skip or repeat rows.
14. As a user, I want the search, filters, sort and page position in the URL, so that I can bookmark and share a view.
15. As a user, I want My Requests as a filter on the shared page, so that I can see just mine.
16. As a game requester, I want Any or one or more platforms, so that any chosen platform can satisfy my Request.
17. As a game requester, I want Any and explicit platforms to be mutually exclusive, so that my Request is unambiguous.
18. As a user, I want poster art to stay poster-shaped and album art square, so that images are not distorted.
19. As a keyboard or screen-reader user, I want every page to be navigable and labelled, so that I can use the portal.
20. As a phone user, I want the pages to lay out well at small widths, so that I can request from anywhere.

### Availability and notifications
21. As a subscriber, I want my Request to become `available` when any Source has the item, so that the status reflects reality.
22. As a subscriber, I want an in-app notification when that happens, so that I find out inside the portal.
23. As an operator, I want one Apprise notification per event, so that external channels are not spammed.
24. As a subscriber, I want exactly one notification per availability event even if a sync retries, so that I am not told twice.
25. As an admin, I want an item present in two libraries to stay available until every covering Source has completed a sync without it, so that one library's removal does not wrongly reopen it.
26. As an operator, I want a failed or partial sync never to remove anything, so that an outage does not erase availability.
27. As an admin, I want the history of availability changes recorded, so that I can see why a Request changed.

### Sources
28. As an operator, I want to connect Plex for movies, TV and music, so that Plex content counts as available.
29. As an operator, I want to connect RomM for games, so that game Requests can be satisfied.
30. As a game requester, I want a Request satisfied when RomM holds the game on any platform I selected, so that OR matching works against a real library.
31. As an operator, I want Jellyfin to run on the same Source interface as the others, so that all Sources behave alike.
32. As an operator, I want to connect Navidrome for music, so that music Requests can be satisfied.
33. As an operator, I want each Source to be authoritative only for the media types it is configured for, so that a Source cannot claim things outside its remit.
34. As an admin, I want inexact matches shown as suggestions for review, so that a wrong guess never marks something available.

### Jobs and operations
35. As an admin, I want every sync recorded as a job run with status, times and errors, so that I can see what happened.
36. As an operator, I want two syncs of the same Source never to overlap, even across a restart, so that data is not corrupted.
37. As an admin, I want a "sync now" action per Source, so that I do not wait for the schedule.
38. As an admin, I want a status view of Sources, jobs and recent errors, so that I can diagnose problems without server logs.
39. As an operator, I want a failed sync to retry on the next scheduled run, so that the system recovers without intervention.

### Setup, settings and access
40. As a first-time operator, I want a guided wizard through General, Authentication, Metadata Providers, Libraries, Notifications, Request Policy, Review and Finish, so that I can set up without editing files by hand.
41. As an operator, I want each connection checked before I continue, so that I find mistakes early.
42. As an operator, I want to leave the wizard and resume later, so that I do not lose progress.
43. As an operator, I want the wizard to tell me clearly which secrets still need an environment change and restart, so that I know what is left (D017).
44. As an operator, I want secrets kept in the environment rather than the database, so that I do not manage encryption keys.
45. As an admin, I want to sign in with OIDC only, so that no local passwords exist.
46. As an admin, I want a new user to start as a Viewer, so that access is granted deliberately.
47. As an admin, I want to promote a Viewer to requester or admin, so that people can request.
48. As an admin, I want to set a user back to Viewer, so that I can revoke request access.
49. As an admin, I want the last admin to be protected from demotion, so that the portal cannot be locked out.
50. As a Viewer, I want to browse Requests but be blocked from creating them, so that the role means something.

### Quality and release
51. As a maintainer, I want CI to run on pushes to `main`, so that regressions are caught.
52. As a maintainer, I want the integration suite against a real database to run in CI and locally, so that SQL changes are tested.
53. As a contributor, I want docs that describe the actual project, so that I am not misled.
54. As a user of the software, I want an AGPL-3.0 license and a real module path, so that I know my rights and the code can be imported (D013).
55. As an operator, I want a container image and release binaries that include the UI, so that I can run the real thing.
56. As an operator, I want upgrade and backup notes, so that I can update safely.

## Implementation Decisions

- **Baseline hygiene.** Rename the default branch to `main` and point CI at it. Adopt AGPL-3.0 and replace the placeholder module path with `github.com/evilperson1337/requestarr` (D013). Refresh CLAUDE.md, the status file and the manual-testing doc so they describe the project as it is. Add a make target for the integration suite and run it once to learn the real baseline.
- **Availability projection (D014, D015).** When a Source sync makes a catalog item available, in the same transaction: update the item's availability, move its open Requests to `available` with a version bump, write the request-level audit event, emit the `RequestAvailable` event, and enqueue subscriber and global notifications exactly once per event. An item becomes missing only after every Source that covers it has completed a successful sync without it. The unused reconcile package is removed or folded into the adapter design; no new features go into it before then.
- **Request transitions.** Add cancellation per D016. Admins can cancel outright; a requester leaves and the Request closes only when the last Subscriber leaves. The existing optimistic-concurrency and idempotency-key behaviour is preserved.
- **Roles.** The role endpoint accepts `viewer`, `requester` and `admin`. Demoting the last admin is rejected.
- **Job runner (D018).** A PostgreSQL-backed run table with status, timestamps and errors; a no-overlap guard that survives restarts; an admin trigger; a status endpoint. No queue library and no separate worker process. Retry is the next scheduled run. Sync for existing Sources moves onto it.
- **Source adapter interface.** One narrow interface for all Sources: given configuration, produce a complete, normalized snapshot of what the Source holds for its configured media types, or an error that is distinguishable from "empty". Each Source declares the media types and identifier kinds it can match. Order of delivery: Plex, RomM, Jellyfin migrated, Navidrome (D019). Matching generalizes beyond movie/TV IDs to music and game identifiers; inexact matches are suggestions only.
- **Requests API and page (contract first).** Update the HTTP contract before the handlers. Server-side title search, status and media-type filters, sort and cursor pagination. All view state is URL-addressable. Cancel and leave actions are added. Error responses use one consistent shape and carry a request identifier.
- **Setup wizard and settings (D012, D017).** Persist wizard draft state so it is resumable. Store non-secret settings and the names of environment variables that hold secrets. Validate each external connection. Provide provider, Source, job and recent-error status views for admins. State clearly what remains to be set in the environment.
- **Release hardening.** The release build includes the web assets. Follow the open items in the releases doc for container policy, supply-chain artefacts and upgrade/backup notes. Accessibility and responsive audit against the final mockups.
- **Constraints carried over.** One shared Request per item; no vote counts; OIDC-only; no acquisition or downloading behaviour; migrations are append-only once merged; domain logic stays free of HTTP, database and provider imports.

## Testing Decisions

A good test exercises external behaviour through a public seam and survives refactoring. It does not assert on internal structure, and it does not build the value it then asserts on.

Four seams, highest first. Prefer the first; use the others only where it cannot see the behaviour.

1. **HTTP API seam.** The router driven with a fake store and fake providers. Covers the role matrix (unauthenticated, viewer, requester, admin) on every mutating endpoint, create, cancel and leave, transitions, search/filter/sort/pagination, job endpoints, wizard and settings endpoints, and last-admin protection. Prior art: the existing router tests and the Requests service tests.
2. **PostgreSQL store seam.** The existing integration suite against a real database. Covers the availability projection in one transaction, request status, events and notification rows, multi-Source availability, failed-sync-never-removes, the job-run no-overlap guard, concurrency and idempotency. Prior art: the existing Jellyfin snapshot idempotency and absence tests. This suite must run in CI and via a make target.
3. **Source adapter contract seam.** One shared suite run over recorded fixtures for every Source: a response becomes a normalized snapshot; a failure is reported as failure, never as empty. Each adapter has success and failure-path fixtures.
4. **Frontend smoke seam.** The main pages rendered against a mocked API: Requests, request modal, notifications, settings and the wizard.

In addition, one scripted Keycloak login check against the dev compose environment. Browser end-to-end tests are out of scope for V1.

## Out of Scope

- Encrypted secrets in the database and key rotation (D017).
- Retry engines, queue libraries and separate worker processes.
- Jellyfin music or other media types beyond what is already covered; Navidrome covers music.
- Voting or vote counts.
- Any downloading, acquisition or indexer behaviour.
- Per-user Apprise routes (deprecated by D006).
- Browser end-to-end test suite.
- Multi-instance Sources of the same type, beyond not hard-coding a single Jellyfin identity.

## Further Notes

- The status report left one finding unverified at the time and the follow-up trace confirmed it: no code path sets a Request to `available` today, and `RequestAvailable` is never emitted.
- Viewer cannot create Requests is already enforced and unit-tested; only the demotion path is missing.
- The Docker image was last verified before admin management was added; re-verify as part of release hardening.
- Node versions differ between CI and the Dockerfile; align them during hardening.
- Rename of the branch and module path touch the remote and every import, so they are done as their own early change, with your go-ahead at that point.
