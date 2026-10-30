# Rich movie request preview

Status: implemented (not deployed)

## Problem Statement

After pasting a movie link, users currently get too little information to
confidently confirm the intended movie before making a Request. The preview
shows a poster, title/year, provider link, and note, but omits useful TMDB
information and provides limited feedback about existing Requests, availability,
and successful submission. A richer preview must remain a fast confirmation
surface rather than become a full discovery interface.

## Solution

Keep the link-first flow and redesign the movie preview as a richer responsive
modal: wider on desktop and full-screen with stacked content on phones. Present
an uncropped poster and restrained backdrop, identity and quick facts, an
expandable official synopsis, and compact director/cast information. Keep the
appropriate next action reachable in a sticky footer.

Use local shared-Request and library availability state to distinguish requesting,
subscribing, and opening an available movie. Optional metadata failures must not
block requesting a movie whose canonical identity is resolved. Leave successful
submission visible in the modal with confirmation, notification status, View
request, and Close. Keep TV, music, and game-specific behavior unchanged.

## User Stories

1. As an authenticated user, I want to paste a TMDB movie link with or without
   its readable title suffix, so that I can use the link copied from TMDB.
2. As a requester, I want a confirmation modal rather than a separate movie page,
   so that I can complete the task without losing my place.
3. As a desktop user, I want a wider, clearly organized preview, so that I can
   scan the movie's information without unnecessary scrolling.
4. As a phone user, I want a full-screen stacked preview, so that information
   and controls remain usable on a narrow display.
5. As a user, I want the full poster without cropping, so that the movie remains
   recognizable and the artwork retains its intended proportions.
6. As a user, I want a restrained backdrop with readable text, so that the
   preview feels informative without sacrificing legibility.
7. As a user, I want title and year prominently displayed, so that I can
   distinguish similarly named movies or remakes.
8. As a user, I want runtime and genres, so that I can quickly verify the movie.
9. As a user, I want release date and status, so that I understand whether the
   movie has been released without confusing that with library availability.
10. As a user, I want a clearly labeled TMDB score and number of ratings, so that
    I understand the provider's rating without mistaking it for Requestarr votes.
11. As a user, I want a short official synopsis with Show more and Show less,
    so that I can choose how much text to read.
12. As a user, I want director names and up to five lead cast members with
    character names when available, so that I can confirm the intended movie.
13. As a user, I want missing facts omitted or honestly described, so that I am
    not misled by invented runtime, release dates, or ratings.
14. As a user, I want neutral artwork fallbacks, so that missing or failed images
    do not make the preview unusable.
15. As a requester, I want optional enrichment failures not to block a Request,
    so that incomplete credits or artwork do not stop the main task.
16. As a user, I want an inline resolution error that preserves my pasted link,
    so that I can correct or retry it without starting over.
17. As a requester, I want an upcoming movie to remain requestable, so that I
    can ask for it before release.
18. As a requester, I want Request movie to remain reachable while details
    scroll, so that richer information does not hide the main task.
19. As a requester, I want an expandable optional note, so that I can add context
    without a large empty input dominating the preview.
20. As a requester, I want submission failures to preserve my note and preview,
    so that I can retry safely.
21. As a requester, I want double-clicks and retries not to create duplicates,
    so that there remains one shared Request for the canonical movie.
22. As a user viewing an already requested movie, I want its shared Request
    state and View request, so that I do not create another Request.
23. As an authenticated user, I want Notify Me or Notifications On as appropriate,
    so that I can follow the existing Request without duplicating it.
24. As a user viewing an available movie, I want library-authoritative
    availability and a usable library link when supplied, so that I can open it.
25. As a user, I want unknown availability to remain explicitly unknown, so that
    temporary failures are not presented as proof the movie is missing.
26. As a requester, I want an in-modal success confirmation and notification
    status, so that I know my action completed before deciding where to go.
27. As a user, I want View request to lead to the correct shared Request, so that
    I can follow up without searching for a possibly ambiguous title.
28. As a viewer, I want only actions my role permits, so that the preview does
    not invite an action the server will reject.
29. As a keyboard or assistive-technology user, I want a labeled modal, visible
    focus, announced state changes, and predictable dismissal, so that I can
    use the whole flow without a pointer.
30. As a phone user entering a note, I want the keyboard and sticky footer not
    to obscure the input or focused control, so that I can finish the Request.
31. As a user, I want a link back to TMDB, so that I can find more provider
    information without adding discovery features to Requestarr.
32. As an existing TV, music, or game user, I want my current request-specific
    controls and behavior preserved, so that the movie redesign is not a regression.

## Implementation Decisions

### Scope and visual hierarchy

- This is the movie-specific presentation of the reusable request preview, not
  a new standalone movie-details route.
- Desktop order: restrained backdrop; complete poster beside title and quick
  facts; synopsis; compact director/cast; expandable note and sticky actions.
- Phone order is a compact single-column equivalent with a full-screen modal
  and reachable close/action controls. Preserve poster aspect ratio, text
  contrast, focus visibility, and safe scrolling over decorative fidelity.
- Do not add autoplay, animated backgrounds, a cast-photo gallery, or a new
  navigation destination solely for the richer preview.

### Metadata and display contract

- Extend the TMDB adapter, catalog metadata mapping, explicit API response
  contract, and movie rendering as one end-to-end path. Persist the mapped movie
  metadata using the existing catalog persistence boundary rather than making
  the browser hold the only copy.
- Carry title/year, official overview, runtime, genres, full release date,
  release status, TMDB score, rating count, poster, backdrop, directors, and up
  to five lead cast members with character names when present.
- Use explicit bounded, validated metadata at public boundaries; do not expose
  raw provider payloads or arbitrary response fields. Preserve canonical
  provider identity and the existing shared-Request invariant.
- Scores use a ten-point scale with the number of TMDB ratings alongside.
  Runtime is compact, for example 2h 14m. Release dates use the user's locale
  without shifting a date-only value to another calendar day.
- Omit unknown optional facts rather than inventing defaults. An unrated movie
  is Not yet rated, not a fabricated zero score; missing runtime is not zero
  minutes. Year and release date remain absent when unknown.
- Use Upcoming and a supplied future release date where applicable; an unknown
  date must not be fabricated. Release status is not availability evidence and
  does not itself prevent requesting.
- Render a short official synopsis with an accessible expansion control for
  long text. No spoiler gate, reviews, or additional plot summary is included.
- The director/cast section remains compact and handles absent or partial
  character names. Do not assume a single director if TMDB supplies several.
- Backdrop is decorative and optional. Keep text on a reliably readable
  surface; use neutral fallback artwork when an image is absent or fails.
- Retain a safe link to TMDB. Image URLs must follow the existing approved
  provider-origin and validation policy rather than accepting arbitrary origins.

### Loading and partial failure

- Preserve link-entry progress until canonical movie resolution completes.
  Failed core resolution retains the entered URL and an inline, safely worded
  error; do not open an unusable modal or enable requesting an unverified item.
- Optional enrichment is not a prerequisite for a usable preview. If fetching
  secondary details fails, render known core information and a small Some
  details unavailable notice without disabling an otherwise permitted Request.
- Normal absence is not a provider failure: do not imply an error merely
  because a movie has no credits, ratings, or backdrop supplied.
- Reuse the resolved/local data during the preview flow. Rendering, expanding
  synopsis or note, and reopening a known preview must not cause repeated TMDB
  calls. Explicit resolution may perform bounded provider work; a new general
  background-refresh system is not required by this feature.
- All external work remains server-side, cancellable, time-bounded, response-
  size-bounded, and restricted to trusted provider endpoints. Optional calls
  must not multiply the total lookup budget or expose credentials to the browser.

### Local Request and availability state

- Supply the preview with local shared-Request, current-user subscription,
  permission, and library-availability information needed for the next action.
  Do not call libraries or acquisition tools to render this state.
- For a new item not known available, show Request movie to an authorized
  requester/admin, an expandable Add a note, and Cancel/Close.
- For an existing shared Request, show its lifecycle state, Notify Me or
  Notifications On as appropriate, and View request. Preserve closed-Request
  subscription rules rather than offering an invalid operation.
- For known available content, show authoritative Source availability and Open
  in library when a usable Source link exists. Do not invent a library link or
  replace unknown availability with missing. If both an existing Request and
  availability exist, expose the useful state without contradicting either.
- Request lifecycle and availability remain separate internally, even when the
  visual hierarchy simplifies them. No change to request policy is authorized.
- View request must target the actual shared Request using supported shared-
  Requests navigation. Include any minimal navigation support needed to make
  that target usable; do not substitute an ambiguous title search or fabricate
  a movie-details route.
- Server role checks remain authoritative. Do not offer movie-creation actions
  to viewers or weaken authentication, CSRF, or Origin protections.

### Submission, success, and dismissal

- Keep the primary action in a reachable sticky footer while details scroll.
  Expand the optional note only when requested, maintaining its existing limit
  and preserving its content across recoverable failures.
- Announce pending state and prevent duplicate in-flight submission. Preserve
  canonical shared-Request idempotency for retries and concurrent creation.
- On failure, preserve the preview and note and show a safe actionable error.
  Authorization loss must remove actionable protected state rather than allow
  continued submission from a stale modal.
- On success, stay in the modal, remove the submit action, confirm the shared
  Request and actual subscription state, and offer View request and Close.
  The creator remains automatically subscribed under existing rules.
- If another user created the Request between preview and submit, use the
  existing Request and actual subscription result; do not claim a second Request
  was created or claim notifications are on without confirmation.
- Preserve accessible focus containment, Escape/Close behavior, focus return,
  and state announcements. Keep the on-screen keyboard, focused input, and
  sticky footer from obscuring each other on phones.

### Compatibility and persistence

- Keep TV, music, and game-specific data and controls unchanged. Movie-only
  success and note behavior must not silently change other media flows.
- Existing catalog records lacking enriched fields must still render and
  resolve safely. Add append-only migrations and migration tests only if the
  chosen durable representation needs a schema change.
- Update the public API documentation and user-facing guidance alongside any
  changed response contract or workflow. No new dependency is assumed.

## Testing Decisions

- The user approved two primary seams: authenticated API behavior through real
  services with deterministic external-provider fakes, and browser request-flow
  behavior with controlled API responses.
- Tests assert observable contracts and outcomes, not private helpers, DOM
  implementation details, or a fixed component hierarchy. Expected values come
  from literal fixtures and these requirements, not the implementation's own
  calculations.
- API/service coverage checks mapped metadata, canonical identity, local state,
  subscription/permissions, failures, and duplicate-safe actions. Use existing
  TMDB resolver/API fixture patterns and the role-matrix/request transition
  tests as prior art. Never require live TMDB, library accounts, or credentials.
- Browser coverage extends the existing link-to-preview-to-request smoke flow
  and keyboard/phone accessibility patterns. Update the movie expectation from
  immediate navigation to in-modal confirmation, preserving non-movie cases.
- Include numeric-only and title-suffixed movie links, old sparse catalog data,
  present/absent details, unrated movies, invalid provider values, upcoming and
  unknown dates, missing/broken images, optional-credit failure, and total
  resolution failure. Distinguish ordinary missing data from provider failure.
- Include new/existing/available/unknown states, allowed and forbidden roles,
  subscription on/off, closed Request rules, no usable Source link, concurrent
  creation, double-submit/retry, note preservation, and authorization loss.
- Verify sticky-footer reachability, synopsis expansion, desktop/phone overflow,
  focus containment/return, close behavior, and announced pending/error/success
  states. Verify no extra provider calls on render or expansion.
- Use credential canaries and hostile provider fixtures to verify tokens,
  Authorization headers, and raw provider errors do not reach response data,
  user messages, logs, or test failure output.
- Add durable-storage contract and migration coverage if persistence changes
  require it; the primary seams do not waive repository migration rules.
- Every ticket includes its own deterministic backend/browser regressions and
  required formatting, full Go tests, vet, and frontend checks. Do not defer
  feature correctness or accessibility to a final testing-only ticket.

## Out of Scope

- TV, music, or game redesign; changed game-platform selection semantics.
- Dedicated movie-details/discovery pages, search or recommendation shelves,
  reviews, trailers, full crew browsing, and large cast-photo galleries.
- Region-specific age certifications or an implicit certification-region policy.
- New voting, per-user duplicate Requests, changes to request approval policy,
  media acquisition, download clients, or external orchestrator integrations.
- Live library scans on modal open, repeated provider calls on render, or a
  general metadata/background-refresh platform.
- OIDC/authentication redesign, automatic credential rotation, destructive
  database resets, deployment, commits, or restarting the user's stack.

## Further Notes

- The user confirmed the complete design after four interview rounds, then
  approved both test seams and the three-ticket vertical-slice breakdown.
- Ticket 1 delivers the core rich preview end-to-end and has no blockers.
- Ticket 2 adds optional director/cast/backdrop enrichment and is blocked by
  Ticket 1.
- Ticket 3 adds state-aware actions, optional note, retry, and success behavior
  and is blocked by Ticket 1, not Ticket 2. Tickets 2 and 3 can proceed
  independently after Ticket 1, while coordinating shared UI/API edits.
- No prefactoring ticket is required. Keep any enabling contract changes
  inside the slice that demonstrates them through working user behavior.
- The title-suffixed TMDB URL parsing defect was handled separately. This
  feature retains its regression coverage rather than depending on live data
  for a particular movie.
- These documents authorize planning. Implementation remains a separate task;
  publishing the plan does not deploy or modify the running application.

## Implementation outcome

All three approved vertical slices are implemented and their tickets are marked
done. Durable movie data uses the existing catalog JSON rather than a new
schema. Local Request/availability state uses an explicit authenticated read
contract, and exact Request-ID filtering supplies View request navigation.
Required Go/frontend checks, the production frontend build, all 165 browser
tests, targeted race checks, and targeted disposable-PostgreSQL tests passed.
No live provider credentials were needed by tests and the running stack remains
unchanged pending a separate rebuild/redeployment.
