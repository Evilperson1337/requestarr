# Movie request preview redesign — design interview

Status: confirmed; specification, test boundaries, and three-ticket implementation plan approved.

## Goal

Enrich the movie request preview with TMDB information so a user can confirm
the intended catalog item and submit a Request confidently.

## Confirmed decisions

### Round 1 — purpose, presentation, scope

- An informative confirmation experience, not a full movie-discovery or
  browsing destination.
- Retain the modal opened after resolving a pasted link; do not introduce a
  dedicated movie-details route for this first redesign.
- Use a wider responsive detail layout on desktop and a full-screen modal
  presentation on phones. Preserve keyboard navigation and clear close/back
  behavior.
- Movies are the initial scope. TV, music, and game-specific information and
  controls remain unchanged; extending the design to TV is a separate decision.

### Round 2 — information and visual treatment

- Show title/year, synopsis, runtime, genres, release date/status, and a clearly
  labeled TMDB score with its number of ratings. These are provider ratings,
  not votes on a Request.
- Prioritize title and quick facts, followed by the synopsis.
- Include the director and up to five lead cast members, with character names
  where available, in a compact section without a large photo gallery.
- Full crew lists, recommendations, reviews, and trailers are out of scope for
  this first version.
- Use a restrained backdrop banner while retaining the complete uncropped
  poster alongside the details. Text stays on a reliably readable surface.
- Stack the content compactly on phones. Preserve poster aspect ratio and do
  not introduce autoplay or animated backgrounds.

### Round 3 — incomplete data, synopsis, actions, upcoming movies

- Omit unavailable optional facts rather than inventing defaults. Missing
  runtime is not zero minutes; missing ratings are not a zero score. Use a
  neutral artwork fallback and an honest not-yet-rated presentation.
- Once canonical identity is resolved, missing cast, synopsis, or backdrop must
  not prevent a Request.
- Show a short official TMDB synopsis by default, with an accessible Show more /
  Show less control for longer text. Do not add a spoiler gate, reviews, or
  additional plot summaries.
- Keep the primary Request movie action in a footer that remains visible while
  details scroll. Retain Cancel/Close. The optional note is expandable behind
  Add a note rather than displaying a large empty textarea by default.
- Existing-request and availability states retain their appropriate actions;
  they must not offer duplicate Request creation as the default action.
- Unreleased movies remain requestable. Show Upcoming and the release date
  when supplied, or an honest unknown-date presentation. Do not conflate
  release status with library availability.

### Round 4 — loading, submission feedback, display conventions

- Keep resolution progress beside the pasted link until the movie's canonical
  identity is resolved. If resolution fails, retain the link and show an inline
  error instead of opening an unusable preview.
- Optional enrichment is not a prerequisite for opening a usable preview.
  When fetching extra information fails, show a small Some details unavailable
  notice without disabling the Request action. Rendering or reopening must not
  cause repeated TMDB calls.
- On successful submission, leave the modal open in a clear confirmation state
  with View request and Close. Remove the submit action and indicate the
  creator's automatic notification subscription.
- On submission failure, preserve the preview and optional note for retry.
- Format TMDB scores on a ten-point scale, for example 7.8/10, with the number
  of TMDB ratings alongside. Format runtime compactly, for example 2h 14m, and
  use a locally formatted full release date.
- Keep the link to TMDB. Region-specific age certifications are out of scope
  for this version; do not silently assume a certification region.

## Consolidated layout and state contract

- Desktop: restrained backdrop banner, full poster beside title and quick
  facts, followed by synopsis and compact director/cast information. Optional
  note and the primary action live near the sticky footer, not in a large
  always-visible input block.
- Phone: full-screen modal with stacked content, an accessible close control,
  and a footer that stays reachable while details scroll. Keyboard focus and
  the on-screen keyboard must not be obscured by the footer.
- New and not known available: Request movie, expandable Add a note, Cancel.
- Already requested: shared Request status, Notify Me or Notifications On as
  appropriate, and View request rather than duplicate creation.
- Known available: library-authoritative availability and Open in library when
  a usable library link exists. TMDB release status is not evidence of library
  availability. Unknown availability must not be labeled missing or available.
- Submitted successfully: requested/subscribed confirmation, View request,
  Close. A duplicate discovered during submission must use the existing shared
  Request rather than claim that a second Request was created.
- Permissions remain server-authoritative. The preview must not offer an
  action the current user is not allowed to perform.

## Existing constraints to preserve

- One shared Request per canonical catalog item; no per-user duplicates or votes.
- Request state and availability are distinct. Libraries, not TMDB, are
  authoritative for availability.
- The universal preview must account for unrequested, already requested, and
  available items, with appropriate actions rather than always creating a new
  Request.
- Preserve poster aspect ratio and accessibility; responsive behavior takes
  precedence over pixel matching.
- Do not make routine UI rendering depend on additional live external calls.
- No acquisition or downloader integration is part of this redesign.

## Current implementation observations

The preview currently shows poster, title/year, provider link, and an optional
note. TMDB resolution already fetches an overview, but the preview does not
display it. Other desired details may require expanding the resolved metadata
contract and its persisted representation.

## Confirmation and implementation boundary

The user confirmed the consolidated design and requested a specification and
implementation tickets. The user then explicitly approved these test boundaries:

- Authenticated API behavior through real services with fake TMDB responses and
  controlled local Request/availability state. This verifies contracts,
  permissions, state handling, and failures, not visual layout.
- Browser request-flow behavior with controlled API responses. This verifies
  visible content, responsive layout, keyboard/focus behavior, notes, and success
  confirmation, not live-provider correctness.

The approved end-to-end tickets are:

1. Rich movie confirmation preview: core metadata through persistence/API to the
   responsive movie preview. No blockers.
2. Director, cast, and backdrop enrichment: optional bounded enrichment through
   provider, API, and UI with partial-failure handling. Blocked by Ticket 1.
3. State-aware request actions and confirmation: local state, appropriate
   controls, expandable note, safe retries, and in-modal success. Blocked by
   Ticket 1, not Ticket 2.

Each ticket includes its own backend and browser tests. Tickets 2 and 3 can
proceed independently after Ticket 1; implementation is separate from publishing
this plan.

Implementation must carry the additional bounded, validated TMDB metadata to
the movie preview and expose local shared-Request/availability state as needed
for the correct actions. These are contract gaps to address, not permission to
make live provider or library calls on every render. Do not change TV/music/game
behavior, introduce discovery browsing, or alter request policy implicitly.

Verification should cover the reported movie-link shape, the selected metadata,
missing/partial metadata, upcoming movies, existing Request and availability
states, permission-appropriate actions, note preservation, duplicate-safe
submission and retry, success confirmation, phone layout, and keyboard/focus
behavior. Live credentials must not be required by automated tests.

These notes record the approved design and planning only. No application code
changes, deployment, credentials, or saved runtime state are changed by publishing
the specification and tickets.

## Implementation record

The user subsequently authorized implementation. All three approved tickets are
implemented, with API/provider/storage/browser regressions and the final checks
recorded in the specification and tickets. The application has not been deployed
or restarted by this work.
