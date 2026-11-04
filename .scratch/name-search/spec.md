Status: ready-for-agent

# Name search: find an item by name instead of pasting a link

Research: `docs/research/SEARCH_INTEGRATION.md`. Vocabulary: `GLOSSARY.md` (**Search result** and **Catalog item** were updated for this feature).

## Problem Statement

Today a person who wants something must already have a provider link (TMDB, MusicBrainz, IGDB or Audible) to start a Request. Most people don't have one in hand: they know the title, not the URL. They have to leave Requestarr, find the item on a provider site, copy its address and come back. That is the main friction between "I want this" and a Request, and it is the one thing comparable request portals (Seerr, DroppedNeedle, GG Requestz, BookOrbit, Listenarr) do that Requestarr does not.

## Solution

Home gets one input, "Search or paste a link". Pasting a supported link works exactly as it does today. Typing a name searches the provider that matches a type the person picks beside the input (Movies & TV, Albums, Songs, Games, Audiobooks). Each **Search result** shows its title, year, a short hint (artist, author, platform) and artwork, plus a status if the item is already requested or already in a library. Picking a result opens the same request preview a pasted link opens, so nothing after that point changes: the same modal, the same Notify Me, the same Request.

Search is a front door to the existing resolver, not a second way to create Requests. It never writes anything. Discovery, trending and recommendations are not part of it.

It ships in phases, lowest provider risk first: Movies & TV (TMDB), then Games (IGDB), then Albums and Songs (MusicBrainz), then Audiobooks (Audible catalog). Each phase is releasable on its own. It is a post-release V1.x feature.

## User Stories

1. As a Requester, I want to type a movie or show title on Home, so that I can start a Request without finding a link first.
2. As a Requester, I want pasted links to keep working in the same box, so that the way I already request things is not disturbed.
3. As a Requester, I want the box to tell a link from a title by itself, so that I never have to switch modes to paste a link.
4. As a Requester, I want to say what kind of thing I am searching for (Movies & TV, Albums, Songs, Games, Audiobooks), so that results come from the right provider and are not a confusing blend.
5. As a Requester, I want my last-used type remembered, so that I do not have to re-pick it every visit.
6. As a Requester pasting a link, I want the type selector to be ignored, so that a link is never misread because the selector was on the wrong type.
7. As a Requester, I want movie and TV results to appear as I type, so that I can stop typing as soon as I see the title.
8. As a Requester, I want game results to appear as I type, so that finding a game is as quick as finding a movie.
9. As a Requester searching albums, songs or audiobooks, I want to press Enter or a Search button, so that slow, rate-limited providers are not hit on every keystroke.
10. As a Requester, I want nothing searched until I have typed at least two characters, so that I do not get noise from a single letter.
11. As a Requester, I want a slow earlier search never to overwrite the results of my latest one, so that I do not click the wrong item.
12. As a Requester, I want each result to show title, year and a type-appropriate hint (artist for music, author and narrator for audiobooks, a platform hint for games), so that I can tell similarly named items apart.
13. As a Requester, I want posters kept 2:3 and music and audiobook artwork kept square, so that results look right for each media type.
14. As a Requester, I want results that are already requested to say so, so that I join the existing Request instead of wondering why it is not new.
15. As a Requester, I want results already in a library to say so, so that I do not ask for something we already have.
16. As a Requester, I want those statuses even for items nobody has opened before, so that the "In library" badge can be trusted.
17. As a Requester, I want statuses shown as text and an icon, not colour alone, so that they are readable regardless of how I see colour.
18. As a Requester, I want already-requested and available items to stay in the list, not disappear, so that an earlier successful request never looks like a missing result.
19. As a Requester, I want to click an already-requested result and get the usual modal with Notify Me, View request and Add to request, so that I can act on it the same way as today.
20. As a Requester, I want to pick a result and see the same preview a pasted link gives me, so that I learn nothing new and lose nothing.
21. As a Requester requesting a game, I want platform selection to still happen in the preview, so that the search row does not have to guess platforms.
22. As a Requester requesting an audiobook, I want results to show narrator, runtime and abridged or unabridged, so that I can pick the right edition.
23. As a Requester, I want multi-part or series audiobooks labelled "Series / multi-part", so that I know what I am picking.
24. As a Requester, I want Albums search to show only albums by default, so that singles and compilations do not bury the album I want.
25. As a Requester, I want an "Include singles & EPs" option on Albums, so that I can still find a single.
26. As a Requester searching songs, I want one row per song with artist, title and length, so that I do not scroll through every release of the same recording.
27. As a Requester searching games, I want main games, remakes, remasters, ports and expanded games shown but DLC, updates and bundles hidden, so that I am not offered things Requestarr does not handle.
28. As a Requester, I want to see a clear message when a provider is busy or unavailable, so that I do not mistake an outage for "no results".
29. As a Requester, I want that message to remind me I can still paste a link, so that I am never stuck.
30. As a Requester, I want a clear "no results" message when a search really returns nothing, so that I can tell it from an error.
31. As a Requester, I want a "searching" state, so that I know something is happening.
32. As a Requester, I want to clear the box with one control, so that I can start over.
33. As a Requester, I want Back to restore my last search, so that I can return to the list after opening a preview.
34. As a Requester, I want to bookmark or share a search URL, so that I can return to it or send it to a housemate.
35. As a Requester, I want the result list and its scroll position to stay in place while the modal is open, so that closing the modal puts me back where I was.
36. As a Requester, I want focus returned to the result I opened when the modal closes, so that I can carry on with the keyboard.
37. As a keyboard or screen-reader user, I want a labelled input, a radio group for type, results as buttons and spoken status updates ("12 results for dune"), so that I can use search without a mouse or sight.
38. As a Viewer, I want to search and open a preview, so that I can see what exists even though I cannot request.
39. As a Viewer, I want the preview's request actions to stay unavailable to me, so that search does not widen what my role can do.
40. As a household member, I want a type to be absent when its provider has not been set up, so that I am never offered a search that cannot work.
41. As an admin, I want search to respect the providers I have already configured in Setup, so that there is no new thing to configure.
42. As an admin, I want search from one person not to starve everyone else of the shared provider budgets, so that one heavy user cannot make previews fail.
43. As an admin, I want identical searches typed at the same moment by several people to cost one provider call, so that a busy household stays within limits.
44. As an admin, I want repeated searches served briefly from memory, so that retyping does not burn provider quota.
45. As an admin, I want a MusicBrainz search never to delay a person who just clicked a result to preview it, so that previews stay fast.
46. As an admin, I want search results never stored in the database, so that provider terms on retaining content are not at risk.
47. As an admin, I want the TMDB logo and the "not endorsed or certified by TMDB" notice shown, so that Requestarr meets TMDB's terms once TMDB titles appear in search.
48. As an operator, I want typed search text escaped for each provider (IGDB queries, MusicBrainz Lucene syntax), so that a typed quote, colon or asterisk cannot alter or break the query.
49. As an operator, I want search text kept out of info-level logs, so that logs do not profile what people look up.
50. As a maintainer, I want every search result guaranteed to be accepted by that provider's resolver, so that picking a result can never fail to resolve for structural reasons.
51. As a maintainer, I want search to be unable to create catalog rows or Requests, so that the one-shared-request-per-item rule cannot be bypassed.
52. As a maintainer, I want the new decision, scope split and wording changes recorded, so that the next person does not read "link-first" and think search is out of bounds.

## Implementation Decisions

**Recording and scope**
- Record a new decision (next free number after D031), "Name search is a front door to the resolver". It says: search returns resolver-acceptable references, creates no new request path, never writes, and is a post-release V1.x feature. D002 stays accepted; the new record supersedes only its "no search" part. It also records the provider-terms assumption below.
- Split the scope document's single "native catalog search/discovery/trending/recommendations" exclusion: name search moves to V1.x; discovery, trending and recommendations stay out of scope.
- Reword every place that says V1 is link-first (the agent rules' accepted decision 1, the project summary, the architecture module list and the fast-path principle) once the decision is accepted. Search is added as a fourth interactive caller of providers beside URL resolution, connection tests and background jobs; routine page rendering still makes no external calls.
- Glossary: **Search result** added and **Catalog item** reworded (already done in this session).

**Domain and provider seam**
- A new provider-side contract, a `Searcher`, sits beside the existing resolver contract in the metadata provider package. It finds candidate items by name, never writes and never creates Requests. A searcher is deliberately not a resolver, so search-only code cannot create catalog rows.
- Each provider package (TMDB, MusicBrainz, IGDB, Audible) implements the searcher next to its resolver and owns its own response shapes, per the existing package conventions.
- A Search result carries: media type, provider, provider ID, a **ref**, title, year, a short subtitle, artwork URL and kind, and a relevance score. A Search result is a pointer, not a Catalog item.
- **Key invariant:** the ref is a value the same provider's resolver already accepts, so resolving a Search result produces exactly what pasting that link would. Refs: TMDB movie or TV URL; MusicBrainz release-group URL for albums and recording URL for songs; IGDB game URL by **slug** (the search must return the slug, not only the numeric ID); Audible product URL or bare ASIN.
- Picking a result calls the existing resolve path. The request creation path still accepts only a URL or a catalog ID. Search metadata is never trusted at request time.
- Search rows are never upserted into the catalog; only resolved items are, as today.

**Search service**
- A search service fans out to the one provider for the selected type, caches, collapses identical concurrent queries into one provider call (single-flight) and annotates results from local state.
- In-process cache keyed by provider, type, normalized text, page and language or region. Fresh for about 60 to 90 seconds; a stale copy is kept up to about 6 hours and used only when the provider is busy or down. Bounded size. Search results are never persisted to the database.
- A per-user rate limit on the search endpoint with built-in defaults. No new admin setting: a type disappears when its provider is not configured, and that is the only switch.
- MusicBrainz is limited to about 1 request per second per IP for the whole instance and is shared with previews, tracklists and alias lookups. Search takes lower priority than a resolve (someone is waiting on a click), has a short queue depth and releases its slot when the browser cancels. IGDB is limited to 4 requests per second and 8 open requests; search uses a small concurrency cap and maps a 429 to the existing "busy" error. TMDB honours 429 likewise.
- Provider failure is never reported as "no results". The endpoint returns the existing provider-busy or not-configured errors, and the UI shows an inline message that also points at the paste path.

**Provider specifics**
- Movies & TV: one TMDB multi-search call; people are dropped. Typeahead.
- Games: IGDB search with limit and offset paging (not a fetch-and-slice). The user text is escaped for the query language, or rejected, so a quote or backslash cannot alter the query. Kinds kept: main games, remakes, remasters, ports and expanded games; DLC, updates and bundles hidden, as a fixed rule. The field holding the game kind is `game_type` (it replaced the deprecated `category`); verify against the live API during implementation instead of guessing the enum. Typeahead.
- Albums: MusicBrainz release-group search, primary type Album by default, with an "Include singles & EPs" toggle (off by default). Rows show type and year. Explicit submit only.
- Songs: MusicBrainz recording search; one row per recording showing artist, title, length and first release, not the list of releases. Explicit submit only.
- MusicBrainz query text is escaped (or the escaping query mode is used) so special characters cannot change the query.
- Audiobooks: the Audible catalog (Audnexus has no title search), using the admin's default marketplace and no per-request region picker. Rows show narrator, runtime and abridged or unabridged; multi-part parents are labelled "Series / multi-part", not hidden. Several editions of one title are shown as separate rows (two ASINs are two Requests, per the audiobook decision); the UI copy states this. Explicit submit only. The endpoint is undocumented, so this phase is last and carries the existing audiobook caveats.
- Provider terms: self-hosted, free, AGPL use is assumed non-commercial for TMDB, MusicBrainz and IGDB. This is recorded in the decision; only the maintainer can change it. Provider image CDNs are hotlinked for result artwork, as previews already do. Open items that do not block phase 1: the Twitch Developer Service Agreement on caching search responses, and Cover Art Archive usage terms.

**HTTP API**
- One new read-only route, `GET /api/v1/search`, with a query text (2 to 200 characters, trimmed, control characters rejected), a required media type (movie_tv, album, song, game, audiobook), an optional page (1 to 10), and for albums the singles-and-EPs toggle. It returns a page of at most 50 results plus a "has more" flag, no total count (MusicBrainz and IGDB cannot cheaply give one), with caching disabled on the response.
- Authenticated session required. Viewers may search; request-creation capability stays gated on the preview.
- Each result optionally carries local state: the catalog ID when one exists, the Request status and the availability. Absent means "never resolved here", not "missing".
- Errors reuse the existing resolve error mapping (invalid input, provider credentials rejected, provider busy with retry-after, provider not configured) plus one new invalid-search code.
- The route is added to the OpenAPI contract and the access table; the existing contract test keeps them in step.

**Local annotation**
- One batched local query per page (at most 50 results) reports Request status and availability. It reads only local PostgreSQL; there are no provider or library calls.
- It matches both on catalog items and on library items' provider identifiers, so an item that exists in a library but was never previewed can show "In library". Library items already store provider identifiers (TMDB, MusicBrainz, IGDB, the Audible ASIN) with an index, but no batched lookup exists yet. It must apply the same rules as resolve-time matching: only the latest complete snapshot of an enabled source counts, movie and TV IDs are separate namespaces, and Plex's MusicBrainz identifier is a release (not release-group) ID, so music needs alias handling.
- The badge is advisory; transactional writes still enforce every rule.

**Frontend (Home)**
- Home's single input becomes "Search or paste a link". If the text parses as a supported URL or ASIN it resolves as today and the type selector is ignored; otherwise it searches.
- A radio group beside the input selects the type, remembered locally, with a type hidden when its provider is not configured.
- Typeahead (300 ms debounce, minimum 2 characters) for Movies & TV and Games; explicit submit for Albums, Songs and Audiobooks. In-flight requests are cancelled and any response that is not the latest is ignored.
- Search state (query, results, busy, error) is kept separate from the link-resolve state, which is keyed to the pasted URL. The existing supported-links footer becomes a list of supported sources.
- Results render as a list of buttons in a live region with distinct loading, empty and error states. The list stays mounted while the modal is open; closing the modal returns focus to the result row.
- The query and type live in the URL so Back restores a search and a search can be bookmarked.
- Status is shown with text and an icon. Poster and square artwork keep their native proportions.
- The TMDB logo and non-endorsement notice are added in phase 1.

**Security and privacy**
- Search text is untrusted: validated for length and control characters, URL-encoded for TMDB and MusicBrainz, escaped for IGDB and for MusicBrainz query syntax. Provider hosts are fixed, so there is no outbound-request surface.
- Search text is not logged at info level; the request ID is enough.

**Phasing (each phase releasable alone)**
1. Decision record and wording updates; TMDB attribution; the searcher contract, search service, route, Home box and type selector for Movies & TV; the batched annotation.
2. Games (IGDB).
3. Albums, then Songs (MusicBrainz).
4. Audiobooks (Audible catalog).

## Testing Decisions

A good test checks behavior a person or API client can observe: what is returned, what is shown, what is written. It does not check which internal function called which, and it does not hit live providers.

Four seams, using the highest existing seam in each layer and no new ones:
1. **HTTP API (main seam).** Tested through the router with fake provider searchers behind it, like the existing resolve handler tests. One suite covers validation, link versus typed-text behavior, Viewer access, provider failure surfacing as errors (never as empty results), caching and stale fallback, single-flight, the per-user limit, and the **ref contract**: for every fixture result, the matching provider's resolver accepts the result's ref. The existing OpenAPI contract test covers the route and the specification staying aligned.
2. **Provider fixtures, per provider.** Sanitized recorded responses under `testdata/`, mapped to Search results against a mock HTTP server, as the existing resolver tests do. This is where IGDB query escaping, MusicBrainz query escaping, the games kind filter, album type filtering, song dedup per recording and Audible multi-part labelling are checked. Live provider tests stay opt-in and out of ordinary CI.
3. **Batched local annotation.** Tested in the existing PostgreSQL integration suite (build tag `integration`, `make test-integration`), because it needs real SQL. It must cover: a library item matched by provider ID with no catalog row, snapshot freshness and enabled-source filtering, separate movie and TV ID namespaces, and MusicBrainz release-versus-release-group alias matching.
4. **Home page in the browser.** Playwright tests, alongside the existing browser tests: the type selector, typeahead versus explicit submit, status badges, keyboard and screen-reader behavior, focus returning to the result row after the modal closes, and the URL state. The debounce and stale-response handling are covered through these tests, not unit tests.

Prior art: the existing resolve handler tests and resolver fixture tests (TMDB, IGDB, MusicBrainz, Audible), the inventory matching tests in the PostgreSQL suite, and the Home page Playwright tests.

Required checks after each phase: `make fmt test vet web-check`, and the browser tests when the UI changes.

## Out of Scope

- Discovery, trending, popular, recommendations, upcoming, genre or studio browsing.
- A blended "All" search across media types (a possible later option, which would need explicit submit and per-provider status).
- A global or cross-provider catalog stored in the database; persisting search results.
- Hiding already-requested or available items (a possible later setting).
- Recent searches, a typeahead dropdown with combobox semantics, and a search-by-ID shortcut.
- A per-request audiobook region picker, and AudiobookDB as a search source.
- DLC, update and edition acquisition workflows for games.
- A new admin setting to disable search per provider, and configurable rate-limit values.
- Any acquisition, download client or indexer behavior (unchanged boundary).

## Further Notes

- Research and sources: `docs/research/SEARCH_INTEGRATION.md`. The reference apps' code was read for behavior only; none of it may enter this repository (GPL/AGPL).
- Known weaknesses to avoid, observed in reference apps: errors swallowed into an empty result page; user text interpolated unescaped into a provider query; client-supplied metadata trusted at request time; a partial local cache answering as if it were the whole catalog; fetch-and-slice pagination.
- Open items that do not block phase 1: Twitch Developer Service Agreement caching terms; Cover Art Archive terms; confirmation of the live IGDB game-kind field name.
- Audiobook search rests on an undocumented endpoint; it is last and should be re-evaluated before it ships.
- Next step: `/to-tickets` to split this into tracer-bullet tickets along the four phases.
