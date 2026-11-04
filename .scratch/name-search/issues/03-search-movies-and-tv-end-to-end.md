# 03: Search Movies & TV by name, end to end

Parent: `.scratch/name-search/spec.md`

**What to build:** A Requester types "dune" into Home's single "Search or paste a link" input with Movies & TV selected, sees matching **Search results** appear as they type, clicks one, and gets the same request preview a pasted TMDB link gives. This is the tracer bullet through every layer.

- A provider-side `Searcher` contract sits beside the resolver contract. A searcher never writes and is deliberately not a resolver. The TMDB package implements it with one multi-search call, dropping people.
- A search service for the selected type caches (fresh about 60 to 90 seconds, stale up to about 6 hours and used only when the provider is busy or down, bounded size, never persisted), collapses identical concurrent queries into one provider call, and maps provider outcomes onto the existing resolve error codes.
- `GET /api/v1/search` is read-only, needs a session, takes query text (2 to 200 characters, trimmed, control characters rejected), a media type and a page, and returns at most 50 results plus a "has more" flag with no store caching. It is added to the OpenAPI contract and the access table.
- Each result carries media type, provider, provider ID, a ref, title, year, subtitle and artwork. **The ref is a TMDB movie or TV URL the existing resolver accepts**, so picking a result runs the existing resolve path unchanged. Search rows are never written to the catalog, and request creation still accepts only a URL or catalog ID.
- Home's input parses a supported URL or ASIN and resolves it as today (the type selector ignored); other text searches. A radio group selects the type (only Movies & TV exists yet, hidden when TMDB is not configured, last choice remembered). Movies & TV searches as you type (300 ms debounce, 2-character minimum, in-flight requests cancelled, stale responses ignored). Search state is separate from link-resolve state.
- A provider failure shows an inline message that also points at the paste path, never "no results". Distinct loading, empty and error states.
- Accessible baseline: labelled input, radio group, results as buttons, a live region announcing counts and status, poster artwork kept 2:3.

Viewers can search and open a preview; request creation stays gated by `canRequest`.

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] Typing a title returns TMDB movie and TV results with title, year and poster; people never appear
- [ ] Pasting a TMDB link still resolves exactly as before, whatever the selector says
- [ ] Clicking a result opens the existing preview and a Request can be created, with no change to the preview, `POST /requests` or Notify Me
- [ ] A contract test asserts the TMDB resolver accepts the ref of every fixture result
- [ ] Provider busy, credentials rejected and not configured surface as errors with the paste-a-link hint, never as an empty list
- [ ] Invalid text (too short, too long, control characters) and an invalid media type get a 400 with the invalid-search code; unauthenticated gets 401; a Viewer can search
- [ ] Identical concurrent queries make one provider call; a repeat within the fresh window makes none; a stale copy is served only when the provider is busy or down
- [ ] Nothing is written to the database by a search
- [ ] The route is in the OpenAPI contract and the access table, and the contract test passes
- [ ] A stale slow response never overwrites a newer search's results (browser test)
- [ ] The type is hidden when TMDB is not configured
- [ ] `make fmt test vet web-check` and the browser tests pass
