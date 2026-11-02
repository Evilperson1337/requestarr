# Audiobook requests via ASIN and Audiobookshelf

Status: needs-triage

Let people request an audiobook by ASIN and see it become Available when Audiobookshelf (ABS) holds it. Decision: D031 (Proposed). Evidence and citations: `docs/research/AUDIOBOOK_INTEGRATION.md`. Domain terms are in `GLOSSARY.md`.

## Boundary

Requestarr only reads ABS. It never calls an ABS write or scan endpoint, never downloads, and never calls BookOrbit (AGENTS.md, V1_SCOPE).

## Decisions

1. **New media type `audiobook`**, square artwork. Identity is the ASIN: `Provider=audible`, `ProviderID=ASIN`. One shared Request per ASIN; different ASINs of one book are different Requests.
2. **Input:** a bare ASIN (validated, upper-cased) or an Audible product URL (`audible.<tld>/pd/<slug-or-nothing>/<ASIN>`). Amazon `/dp/` URLs are not supported in v1 (not confirmed to work for audiobooks).
3. **Region:** admin-set global default marketplace (US by default); an Audible URL's TLD overrides it. Region is display data and never part of the match key.
4. **Metadata:** Audnexus first, Audible catalog as fallback, behind one resolver interface. On failure the Request is accepted with an unresolved placeholder title and resolution is retried later. Resolve only on Request creation, cache results.
5. **Source kind `audiobookshelf`:** URL plus API key (Stored secret, D024), ABS >= 2.26.0, header auth only. Book libraries only. An item counts only when `numTracks > 0`; `isMissing` is absent. Complete snapshot or nothing (D015).
6. **Matching:** exact ASIN equality, plus the existing reviewable title/author candidates for library items without an ASIN. ISBN aliasing is v1.1.
7. **Out of v1:** BookOrbit, region aliasing between ASINs, Socket.IO updates, audiobook Issue kinds, Amazon URLs.

## Tickets

See `issues/`. 01 and 02 are independent; 03 needs 01; 04 needs 01 and 03; 05 needs 02 and 04; 06 closes out.
