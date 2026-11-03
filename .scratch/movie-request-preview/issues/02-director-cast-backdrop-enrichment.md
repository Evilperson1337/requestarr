# 02: Director, cast, and backdrop enrichment

**What to build:** A user can identify a movie more confidently through a restrained backdrop and a compact director/cast section, without losing the ability to preview or request it when optional enrichment is unavailable. Deliver provider enrichment, durable metadata/API mapping, and visible responsive behavior together.

**Blocked by:** 01 — Rich movie confirmation preview.

**Status:** done

## Acceptance criteria

- [x] The preview shows director names and up to five lead cast members, with character names when supplied. Handle multiple directors and partial character data without inventing values.
- [x] Show a restrained optional backdrop while keeping the full poster uncropped. Text remains on a reliably readable surface; phones retain a compact stacked layout with no horizontal overflow.
- [x] Missing or broken backdrops have a neutral fallback. Do not introduce autoplay, animated backgrounds, a cast-photo gallery, full crew browsing, reviews, trailers, or recommendations.
- [x] Map only bounded, validated credits and approved-origin artwork into catalog metadata and the explicit authenticated API response; do not forward raw credits payloads or arbitrary URLs.
- [x] Preserve core movie metadata and the ability to request when optional enrichment fails. Show a small Some details unavailable notice for an actual secondary-fetch failure, not merely because TMDB supplied no credits or image.
- [x] Additional provider work is server-side, cancellable, and bounded by a sensible overall deadline and response limits. It must not multiply the total lookup budget or block a usable core preview indefinitely.
- [x] Reuse resolved/persisted enrichment within the preview flow. Reopening, scrolling, synopsis expansion, or note expansion does not trigger repeated TMDB calls. No general background-refresh platform is required.
- [x] Existing sparse catalog records and movies without credits/backdrop continue to work. Keep non-movie behavior unchanged.
- [x] API/service tests exercise successful, missing, malformed, oversized, slow, and failed enrichment with deterministic external fakes, and verify trusted origins and credential secrecy without real provider accounts.
- [x] Browser tests exercise director/cast content, missing character names, unavailable enrichment, broken artwork, readable text, desktop/phone layouts, and still-reachable request controls.
- [x] Update affected API/user guidance and pass required formatting, full Go tests, vet, and frontend checks; add migration tests if schema changes are necessary.

## Independence

This ticket does not depend on Ticket 3. Keep the enrichment section compatible
with the shared action area and coordinate any overlapping UI/API edits, but do
not introduce an artificial dependency on state-aware actions.

## Completion and validation

Implemented validated backdrop URLs, deduplicated directors and up to five cast members, plus optional credits enrichment with a 1.5-second sub-budget inside a nine-second overall lookup and reserved headroom near the deadline. Null, malformed, wrong-content-type, oversized, timeout, and non-success responses preserve core metadata with an honest notice. Redirects are rejected and injected clients are copied rather than mutated. Deterministic regressions include missing credits, canary secrecy, and persistence.

Validation passed: required formatting, full Go tests, vet, frontend typechecks,
production frontend build, all 165 browser tests, targeted race tests, and
targeted PostgreSQL tests against a disposable database. Review regressions
also caught and fixed link contrast, non-movie focus return, null credits,
near-deadline enrichment, and encoded credential-bearing library links.

Implementation is not deployed. The running stack was not rebuilt or restarted;
no application database, credentials, encryption key, realm, or commits were
changed. Test databases were removed. Existing unrelated work was preserved.
