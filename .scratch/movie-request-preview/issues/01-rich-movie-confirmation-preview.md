# 01: Rich movie confirmation preview

**What to build:** After pasting a movie link, a user can confirm the movie in a richer responsive preview with core TMDB details, an uncropped poster, readable synopsis, and honest missing-data fallbacks. Deliver the complete path from provider mapping through catalog persistence and authenticated API response to the visible preview; this is not a backend-only preparatory ticket.

**Blocked by:** None (can start immediately).

**Status:** done

## Acceptance criteria

- [x] Numeric-only and title-suffixed TMDB movie links open a labeled movie-confirmation modal with the same canonical catalog identity; existing parsing safeguards remain intact.
- [x] Core metadata includes title/year, poster, official overview, runtime, genres, release date/status, TMDB score, and number of TMDB ratings. Data is bounded, validated, persisted through the catalog boundary, and explicitly represented in the public response contract without raw provider payloads.
- [x] Desktop uses a wider detail layout with an uncropped poster beside title/quick facts; phones use a full-screen stacked modal. There is no new movie-details route.
- [x] The official synopsis is short by default with accessible Show more / Show less for long text, without a spoiler gate.
- [x] Score is clearly TMDB's rating on a ten-point scale with its rating count, not Requestarr votes. Runtime is compact and release dates are locale-formatted without a date-only timezone shift.
- [x] Missing optional facts do not become fake zero values or invented dates. Unrated movies have an honest Not yet rated presentation, and missing/broken posters have a neutral fallback.
- [x] Upcoming movies are identified honestly and remain requestable under existing permission/policy rules. TMDB release status is never used as library-availability evidence.
- [x] Link-entry progress and core-resolution errors remain inline, preserving the pasted link. A valid core movie can be previewed when optional facts are absent; rendering or expanding the synopsis does not trigger repeated TMDB calls.
- [x] Core metadata already in the local catalog is reused within the preview flow; older catalog rows without new fields remain compatible. Add append-only migrations and migration tests only if needed by the chosen persistence representation.
- [x] Keyboard focus enters and stays in the modal, close/Escape behavior remains usable, focus returns on dismissal, and content fits desktop/phone widths. Leave room for the approved optional artwork/credits section and sticky action area without implementing the later tickets' behavior prematurely.
- [x] Existing request creation continues to work. TV, music, and game-specific controls and behavior remain unchanged.
- [x] Authenticated API regressions use real services with deterministic TMDB fixtures for mapping, canonical identity, missing/invalid metadata, permissions, bounded failures, and credential secrecy. Browser regressions cover the visible movie preview, synopsis, fallback, phone, and keyboard behavior.
- [x] Update API/user guidance and pass required formatting, full Go tests, vet, and frontend checks. No live credentials, unrelated refactor, new discovery feature, or stack restart is required.

## Scope and handoff

This slice is demonstrable with a real movie-confirmation preview and core
metadata. Director/cast/backdrop enrichment belongs to Ticket 2. Local
Request/availability actions, expandable note, and new in-modal submission
confirmation belong to Ticket 3. Each dependent ticket supplies its own
end-to-end tests; neither may rely on a future testing-only ticket.

## Completion and validation

Implemented typed bounded movie details through the TMDB adapter, existing catalog JSON persistence, explicit API schemas, and the responsive movie-only modal. Added synopsis expansion, date/runtime/rating formatting, upcoming and sparse-data behavior, neutral artwork fallback, and keyboard/phone coverage. API and browser regressions were observed failing before the mapping and UI changes, then passing. Existing TV/game smoke flows remain green; no schema migration was required.

Validation passed: required formatting, full Go tests, vet, frontend typechecks,
production frontend build, all 165 browser tests, targeted race tests, and
targeted PostgreSQL tests against a disposable database. Review regressions
also caught and fixed link contrast, non-movie focus return, null credits,
near-deadline enrichment, and encoded credential-bearing library links.

Implementation is not deployed. The running stack was not rebuilt or restarted;
no application database, credentials, encryption key, realm, or commits were
changed. Test databases were removed. Existing unrelated work was preserved.
