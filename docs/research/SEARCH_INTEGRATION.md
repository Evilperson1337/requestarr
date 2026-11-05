# Search-by-name integration research

Recorded 2026-11-04. Question: users must paste a provider link to start a Request. How should Requestarr add "search by name", as Seerr (Overseerr/Jellyseerr), DroppedNeedle, GG Requestz, BookOrbit and Listenarr do, without breaking its product boundaries?

Method: primary sources only. Upstream source was read from shallow clones at these revisions: Seerr `seerr-team/seerr` `51f6627` (MIT, `package.json:35`), DroppedNeedle `DroppedNeedle/DroppedNeedle` `cf7278a` (AGPL-3.0, README badge), GG Requestz `XTREEMMAK/ggrequestz` `afb42fb` (GPL-3.0, README), BookOrbit `bookorbit/bookorbit` `76450d6` (AGPL-3.0, see the audiobook research), Listenarr `Listenarrs/Listenarr` `38fc183` (AGPL-3.0, `README.md` badge). Provider facts come from official documentation pages fetched on 2026-11-04; "observed" marks a live HTTP call made the same day. Behaviour is described, not copied; none of that code may enter this repository (AGENTS.md, D013). Repo-side claims cite `path` in this checkout. Anything not determinable says "Could not be determined".

Two reference apps are acquisition tools (DroppedNeedle drives download clients, Listenarr drives indexers and torrent/NZB clients, BookOrbit has its own download clients). Only their search and request-entry behaviour is relevant here; Requestarr's boundary (AGENTS.md, D001) is unchanged.

## Verdict

**Viable, small, and compatible with the existing request path, but it needs a decision record.** Search is a new *front door* to the resolver, not a new domain path. The recommended shape:

1. A provider-side `Searcher` interface beside `metadata.Resolver`, returning lightweight `SearchResult`s whose key field is the **canonical URL the resolver already accepts** (or the bare ASIN). Selecting a result calls the existing `POST /resolve`, so the preview modal, `catalogId` upsert, `POST /requests`, subscriptions and D027 selections stay byte-for-byte unchanged. This literally satisfies D002's "native discovery can be added later without changing the request modal/domain path".
2. One new read endpoint, `GET /api/v1/search`, that never writes. Results are annotated from local PostgreSQL only (already requested, already available) so the UI can show "Requested" / "In library" before the person opens anything.
3. One input on Home: **"Search or paste a link"**. If the text parses as a supported URL or ASIN, behave as today; otherwise search. Seerr does the same kind of thing with a `tmdb:<id>` pattern (`server/lib/search.ts`, `server/routes/search.ts`).
4. Phase it: movies and TV (TMDB) first, then games (IGDB), then music (MusicBrainz), then audiobooks (Audible catalog, the weakest source). Per-type selector, not a blended "all" list, until the quiet providers are proven.

**Settled by D032 (2026-11-04):** name search is a post-release V1.x feature, the front-door shape below is accepted, and discovery stays out. The open questions below are marked settled or open.

Search is explicitly **out of V1** (`docs/V1_SCOPE.md`, "Native catalog search/discovery/trending/recommendations"; D002; AGENTS.md accepted decision 1). So this is a post-V1 feature, or a V1 scope amendment. Discovery (trending, recommendations) should stay out: it is a separate, much larger feature and not what was asked.

## 1. How the reference apps do it

### Seerr (Overseerr/Jellyseerr lineage), `seerr-team/seerr`

Backend (`server/routes/search.ts`):

- `GET /api/v1/search?query=&page=&language=`, session-gated (`server/routes/index.ts:157`). Documented in `seerr-api.yml` under `/search` (params `query`, `page`, `language`; response `{page, totalPages, totalResults, results[]}` where each result is a movie, TV show or person).
- One TMDB `search/multi` call (`server/api/themoviedb/index.ts`, `searchMulti`, `include_adult` false by default). Errors in that wrapper are swallowed into an empty page, so a TMDB outage looks like "no results" (a behaviour to avoid; see section 3.7).
- A pattern shortcut: a query containing `tmdb:<digits>` is looked up by ID instead of text (`server/lib/search.ts`, `findSearchProvider`).
- Annotation: results are joined to local `Media` rows via `Media.getRelatedMedia` (`server/entity/Media.ts:37`) using `(tmdbId, mediaType)`, and each result carries `mediaInfo` with a status enum `UNKNOWN, PENDING, PROCESSING, PARTIALLY_AVAILABLE, AVAILABLE, BLOCKLISTED, DELETED` (`server/constants/media.ts:14`). No extra provider calls are made for annotation.
- TMDB access is rate-limited client-side (`maxRequests: 20, maxRPS: 50`) and cached in an in-process cache (`server/api/themoviedb/index.ts:190-196`).
- Request creation does not trust the search payload: `POST /request` receives a `mediaId` and `mediaType` and re-fetches the movie or show from TMDB itself (`server/routes/request.ts:169-172`).

Frontend:

- A header search input (`src/components/Layout/SearchInput/index.tsx`) with a `type="search"` field and an `sr-only` label, a clear button, `autoComplete="off"`.
- Debounce is **300 ms** (`src/hooks/useDebouncedState.ts` default). Typing navigates to a full `/search?query=` page (push the first time, `replace` afterwards so Back is not flooded), `src/hooks/useSearchInput.ts`.
- The results page (`src/components/Search/index.tsx`) is an infinitely scrolling poster grid via the shared `useDiscover` hook; the status badge on each card comes from `mediaInfo`. Search never hides already-requested or available items (`hideAvailable: false, hideRequested: false`); the discover pages can.
- Discover (`server/routes/discover.ts`: `/movies`, `/tv`, `/trending`, upcoming, genre/studio/network sliders) is a separate set of endpoints; it is the part Requestarr should not copy.

Lessons: one multi-search call, annotate from local state, re-fetch on request, show the status on the card instead of hiding items, and tolerate the "empty-on-error" trap by distinguishing errors from no results.

### DroppedNeedle (music), `DroppedNeedle/DroppedNeedle`

Provider: MusicBrainz, "Search the full MusicBrainz catalogue" (`README.md`).

Backend (`backend/api/v1/routes/search.py`, `backend/services/search_service.py`):

- `GET /search?q=&limit_artists=&limit_albums=&buckets=artists,albums` returns grouped buckets (artists, albums). `GET /search/{bucket}?q=&limit=&offset=` pages one bucket (limit 1-100, default 50). `GET /search/suggest?q=&limit=` (min 2 characters, limit 1-10, default 5) serves the typeahead with a 3 s deadline (`SUGGEST_TIMEOUT_SECONDS`). `GET|POST /search/enrich/batch` fills artwork and details afterwards, by MBID batch.
- Results are cached 90 s fresh (`SEARCH_CACHE_TTL`) and kept 6 hours as a stale fallback (`SEARCH_STALE_CACHE_TTL`).
- Ownership annotation: `_apply_album_ownership` sets `in_library` per album from the local library by release-group MBID with a title/artist/year fallback; `requested` is hard-wired false in this path.
- Degradation is explicit: the response carries `service_status` / `remote_status` values (`timeout`, `error`) rather than an empty list when MusicBrainz fails (`suggest`).
- MusicBrainz is throttled by a process-wide token bucket at 1 request per second with priorities, a rate-limit-header recorder and 429 handling (`backend/repositories/musicbrainz_base.py`, `mb_rate_limiter = TokenBucketRateLimiter(rate=1.0, capacity=1)`). A mirror can bypass it only for non-official hosts.
- Cover art is prefetched in the background after a search (`schedule_cover_prefetch`), not inline.

Frontend (`frontend/src/lib/components/SearchSuggestions.svelte`): a debounced query, minimum 2 characters, a dropdown implemented as an ARIA combobox (`role="combobox"`, `aria-expanded`, `aria-controls`, `aria-activedescendant`, `role="listbox"`, `role="option"`, `aria-selected`), then a full results page with separate Artists and Albums buckets (`routes/search/albums`, `routes/search/artists`).

Request identity is the MusicBrainz release group (album) matching D020, which is encouraging: Requestarr's canonical music ID is the same object a MusicBrainz release-group search returns.

Lessons for music: bucket by entity; short typeahead on a separate, tighter budget than the full search; cache fresh 90 s plus a long stale window; surface "MusicBrainz is slow" instead of "no results"; cover art must not be on the search critical path.

### GG Requestz (games), `XTREEMMAK/ggrequestz`

Provider: IGDB (`src/lib/igdb.js`); RomM/Gaseous/Retrom for library cross-reference (`README.md`).

- Search is an Apicalypse `search` query with `limit`, requesting name, slug, summary, release date, rating, cover, platforms, genres, websites and age ratings in one call (`searchGamesByTitle`, `src/lib/igdb.js:150`). **The user's text is interpolated directly into the query string** (`search "${title}";`) with no escaping. Requestarr must escape `\` and `"` or reject them (section 3.7).
- A client-side minimum interval of 100 ms between IGDB calls (`MIN_REQUEST_INTERVAL`, `igdb.js:39`), well under IGDB's documented 4 per second; there is no 429 handling beyond throwing.
- A local cache table is consulted first: if the cache holds at least `min(limit, 5)` partial matches it answers without calling IGDB, otherwise IGDB is queried and results are upserted into the cache in the background (`src/lib/gameCache.js:189-222`). That makes a search over a small cache look "complete" when it is not. Redis or memory sits in front, TTL 15 minutes (`src/lib/cache.js`).
- Pagination is faked: `limit = min(perPage * page, 100)` is fetched and sliced (`src/routes/api/search/+server.js`). There is no IGDB `offset` use, so page 6+ is unreachable.
- `src/components/SearchBar.svelte`: debounce 300 ms, a configurable minimum length, suggestions dropdown. `?autocomplete=true` returns names only, for queries of two or more characters, limit 5.
- Request submission accepts `igdb_id` plus a client-supplied `game_data` object and caches it (`src/routes/api/request/+server.js:171-203`). A client-forged payload can poison the cache. Requestarr's `POST /requests` already takes a `catalogId` or `url` and resolves server-side; keep it that way.

Lessons for games: IGDB search is cheap but must be escaped and paged with `offset`; do not let a local cache pretend to be the catalog; platforms come from the resolver on selection (D009), not from the search result, so the search row only needs a short platform hint.

### BookOrbit (books/audiobooks), `bookorbit/bookorbit`

BookOrbit has its own request system (`server/src/modules/book-request`), so this describes its request-entry search, not anything Requestarr should integrate with (D031 declines BookOrbit as a Source).

- Search is an explicit form with **title and author fields and a Search button**, not typeahead (`client/src/features/book-requests/components/RequestSearchPanel.vue`, `runSearch`). It searches several metadata providers and streams results provider by provider (`client/src/features/book/composables/useMetadataSearch.ts`), with per-provider status so an interrupted provider is not read as "empty".
- Annotation: results are checked in a batch against the library and the request queue via `POST /api/v1/book-requests/availability`, capped at **50 items** per call and documented as "an annotation pass, not a bulk query" (`server/src/modules/book-request/dto/check-availability.dto.ts`, `book-request.controller.ts:90`).
- Where nothing is found it offers a typed free-text request, with a debounced 350 ms hint that checks whether the same text is already in the library or already requested (`RequestSearchPanel.vue`, `FREE_TEXT_HINT_DEBOUNCE_MS`). A generation counter discards stale answers.
- Submission carries provider key and provider ID plus ISBNs, bounded by DTO length limits (`dto/create-book-request.dto.ts`).

Lessons: an explicit-submit search is a respectable choice for slow, rate-limited providers; batch-annotate at a hard cap; stale-answer protection (a generation counter or `AbortController`) is needed whichever debounce is used.

### Listenarr (audiobooks), `Listenarrs/Listenarr`

An acquisition tool; only metadata search is relevant.

- Endpoints: `GET /api/v{n}/search/audible?query=&region=&language=` and `GET /search/title?query=&region=&limit=` (`listenarr.api/Features/Search/SearchController.cs`), plus series lookups.
- The Audible search is a call to `https://api.audible.<tld>/1.0/catalog/products/` with `keywords`, `title`, `author`, `narrator`, `publisher`, `num_results` (clamped 1-50), `page` (0-based), `products_sort_by` (default `Relevance`) and `response_groups` (`listenarr.application/Metadata/Audible/AudibleProductSearchWorkflow.cs`, `SearchProductsCoreAsync`, lines ~198-219), with a 10 s timeout.
- Front end keeps an "Add New" search in a store with a request id so only the latest search may apply its results (`fe/src/stores/addNewSearch.ts`).

Lessons: Requestarr's audiobook resolver already uses the same Audible catalog host (`internal/providers/metadata/audible/resolver.go`), so audiobook search reuses one client; the latest-search-wins guard recurs in every UI that works.

### Summary of patterns

| Concern | Seerr | DroppedNeedle | GG Requestz | BookOrbit | Listenarr |
| --- | --- | --- | --- | --- | --- |
| Trigger | typeahead, 300 ms | typeahead, min 2 | typeahead, 300 ms | explicit submit | explicit submit |
| Result container | page grid | dropdown + page | page grid | list panel | list |
| Buckets | movie/tv/person mixed | artists, albums | one | by provider | one |
| Local annotation | status on card | `in_library` | partial | batch availability, max 50 | none relevant |
| Server-side request re-fetch | yes | not examined | **no** (client payload) | provider keys | n/a |
| Failure surfacing | empty page (weak) | explicit status | log + empty | per-provider status | HTTP 500/404 |

"Dropped Needed", as named in the original request, is DroppedNeedle. No other project of that name was found.

A fork, `snapetech-org/seerrng` (Seerr adding music, ebooks, audiobooks, games), exists per a web search, but it was not read; treat it as unexamined.

## 2. Provider search APIs

### TMDB (movies, TV)

- `GET https://api.themoviedb.org/3/search/multi` with `query` (required), `include_adult` (default false), `language` (default `en-US`), `page` (default 1). Response `{page, results[], total_pages, total_results}`; items carry `media_type` plus movie fields (`title`, `release_date`) or TV fields (`name`, `first_air_date`), `poster_path`, `popularity`, `vote_average`, `vote_count`, `overview`, `genre_ids`. The page shows 20 results on page 1; a maximum page count is not documented. Source: https://developer.themoviedb.org/reference/search-multi.
- Per-type endpoints: `search/movie` (params also `primary_release_year`, `region`, `year`) https://developer.themoviedb.org/reference/search-movie and `search/tv` (`first_air_date_year`, `year`) https://developer.themoviedb.org/reference/search-tv. Use these when a type filter is selected; `search/multi` also returns people, which must be dropped.
- Auth: Bearer read token in `Authorization` (same as the resolver, `internal/providers/metadata/tmdb/resolver.go:85`).
- Rate limit: the old 40 per 10 seconds cap was removed on 2019-12-16; "somewhere in the 40 requests per second range" remains, may change, and clients must honour HTTP 429. https://developer.themoviedb.org/docs/rate-limiting.
- Images: build `<base_url>/<size>/<file_path>` from `/configuration`; examples use `https://image.tmdb.org/t/p/w500/...`. https://developer.themoviedb.org/docs/image-basics.
- Terms (https://www.themoviedb.org/api-terms-of-use): attribution is a condition of the licence (§1.B, §3), including the TMDB logo and a "not endorsed, certified, or otherwise approved by TMDB" notice; do not cache TMDB content longer than 6 months (§1.C); no commercial use without a separate agreement (§2.A). Search results and resolved metadata are both TMDB Content. The attribution notice is a UI requirement that applies as soon as TMDB titles appear in search; whether Requestarr's current UI already shows it could not be determined from the sources read here.

### MusicBrainz (albums, songs)

- `GET https://musicbrainz.org/ws/2/<type>?query=<Lucene>&fmt=json&limit=&offset=`; `type` is `release-group` or `recording` (also artist, release, work). `limit` 1-100, default 25; `offset` pages; `fmt=json` selects JSON (or `Accept: application/json`; `fmt` wins). `dismax=true` switches to a parser that escapes special characters; default is edismax with full Lucene syntax. https://musicbrainz.org/doc/MusicBrainz_API/Search and https://musicbrainz.org/doc/MusicBrainz_API.
- Release-group fields include `releasegroup`, `artist`, `artistname`, `primarytype`, `secondarytype`, `firstreleasedate`, `rgid`; recording fields include `recording`, `artist`, `release`, `rgid`, `rid`, `isrc`, `dur`. Unfielded terms search `releasegroup` or `recording` respectively. Same page.
- Response `{created, count, offset, <entity>-list}`, each item with a relevance `score`. Observed 2026-11-04 for `release-group?query=releasegroup:"dark side"&limit=2`: top-level keys `created, count, offset, release-groups`; `count` 589; item keys `id, title, score, primary-type, first-release-date, artist-credit[], releases[]`, so artist, year and type arrive in the search row with no second call.
- Rate limit: about **1 request per second per IP** on average; above that, all requests from the IP are declined with 503 until the rate drops. A meaningful `User-Agent` with a contact URL or email is required; anonymous agents are limited to about 50 per second. https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting. Requestarr already sets `Requestarr/1.0 (+https://github.com/evilperson1337/requestarr)` (`internal/providers/metadata/http.go:111`) and has a shared 1 per second spacer (`internal/providers/metadata/musicbrainz/spacer.go`).
- Terms: "Non-commercial use of this web service is free"; commercial use needs a plan (https://musicbrainz.org/doc/MusicBrainz_API). Core data is CC0; supplementary data (including tags, ratings) is CC BY-NC-SA 3.0 (https://musicbrainz.org/doc/About/Data_License). Cover Art Archive images are already used by the resolver (`internal/providers/metadata/musicbrainz/resolver.go:110`); its terms were not read here.
- Consequence: the 1 per second budget is **shared with the resolver, the tracklist lookup and alias lookups (D020)** in the same process. Search is the first feature that can be triggered by keystrokes, so it needs its own priority and cancellation, section 3.6.
- Release-group search returns rows for singles, EPs and compilations; the UI should show the type and year and may filter `primarytype:album` by default. A song search uses `recording`, which returns the same recording under many releases; the canonical ref is the recording ID (D020), so the row should present artist, title, length and the first release, not the release list.

### IGDB (games)

- `POST https://api.igdb.com/v4/games` with a plain-text Apicalypse body, e.g. `search "zelda";` plus `fields`, `where`, `limit` (default 10, max 500) and `offset`. Results are "sorted by similarity to the given search string". Searchable endpoints include Games, Platforms, Collections. https://api-docs.igdb.com/ (sections Search, Pagination).
- Auth: a Twitch app Client ID and Secret; `POST https://id.twitch.tv/oauth2/token?client_id=&client_secret=&grant_type=client_credentials` returns `access_token` and `expires_in`; send `Client-ID` and `Authorization: Bearer <token>`. Same page; the repository already implements this (`internal/providers/metadata/igdb/resolver.go:163`).
- Rate limit: **4 requests per second**, HTTP 429 above it, **up to 8 open requests** at once. Same page ("Rate Limits"). Multi-Query (`/v4/multiquery`) allows at most 10 queries per request.
- Images: `https://images.igdb.com/igdb/image/upload/t_{size}/{hash}.jpg`; `cover_big` is 264x374, `thumb` 90x90, `micro` 35x35. Same page; matches the existing cover URL (`resolver.go:188`).
- Game kind: the Game object has a `category` enum (0 main game, 1 DLC/add-on, 2 expansion, 3 bundle, ... 8 remake, 9 remaster, 10 expanded game, 11 port, 14 update). The documentation also announces a migration of enum fields to tables, including a new `game_types` endpoint, with both names available from February 18 to August 31 (year not stated on the page) and the old names removed afterwards. Because today is after that window, **verify whether `category` or `game_type` is the live field before writing the filter**. Requestarr scope excludes DLC and update workflows (`docs/V1_SCOPE.md`, "Game DLC/update/edition acquisition workflows"), so the filter should keep main games, remakes, remasters and ports, hide DLC and updates, and be a setting if edge cases matter. Do not guess the enum from this document.
- Terms: free "for non-commercial usage under the terms of the Twitch Developer Service Agreement"; commercial needs a partnership (https://api-docs.igdb.com/). The agreement itself was not read; whether it limits caching search responses could not be determined.
- The resolver identifies a game by slug in the URL (`https://www.igdb.com/games/<slug>`, `resolver.go:180`). So the search row must return the **slug**, not only the numeric ID, to build a ref the resolver accepts.
- Escaping: Apicalypse strings are double-quoted; the documentation shows no escaping rules. Escape `\` and `"` in the user text or reject them, since GG Requestz's unescaped interpolation (above) shows the failure mode. Treat the Apicalypse body as an injection surface (AGENTS.md: provider payloads and inputs are untrusted).

### Audible catalog (audiobooks)

- The audiobook research settles on Audnexus first and the Audible catalog as fallback for ASIN lookup (`docs/research/AUDIOBOOK_INTEGRATION.md` section 3, D031). **Audnexus has no title search** (its documented routes are `/books/{ASIN}`, `/authors`, `/books/{ASIN}/chapters`, per https://api.audnex.us/docs as cited there), so name search must use the Audible catalog or AudiobookDB.
- Observed 2026-11-04: `GET https://api.audible.com/1.0/catalog/products/?keywords=wizards+first+rule&num_results=2&response_groups=media,contributors,product_attrs` returned HTTP 200, `cache-control: private, max-age=1800`, and a body with `products[]`, `total_results` (64) and `response_groups`. Each product had `asin`, `title`, `subtitle`, `authors[].name`, `narrators[]`, `format_type`, `release_date`, `runtime_length_min`, `language`, `product_images`, `has_children`, `content_delivery_type` (`MultiPartBook` here). Query parameters (`keywords`, `title`, `author`, `num_results` up to 50, `page` 0-based, `products_sort_by`) are confirmed only by Listenarr's source (above) and this one probe.
- **It is undocumented**: no published terms, key or rate limit (`docs/research/AUDIOBOOK_INTEGRATION.md` section 3, "Audible catalog API"). Search multiplies calls compared to resolve-on-submit, so this is the highest-risk provider for a typeahead. Recommendation: explicit submit only, never typeahead, and cache.
- `has_children` / `content_delivery_type` indicate multi-part or series parents, matching the "box set" risk already recorded (audiobook research, Gaps 4; D031 follow-ups). Show these as "Series / multi-part" rather than hiding them.
- Region: the search must use the admin default marketplace (`REQUESTARR_AUDIBLE_REGION`, D031); no per-request picker.
- AudiobookDB has a documented search but keyed access, attribution and no-long-term-caching terms that D031 already rejects as a default dependency.

## 3. How search plugs into Requestarr

### 3.1 Provider interface

Today a `metadata.Resolver` recognises a URL and resolves it (`internal/providers/metadata/provider.go`), and `metadata.Service` caches resolved items for 10 minutes fresh and 24 hours stale-on-busy. Add, in the same package, a sibling contract (names are proposals):

```go
// Searcher finds candidate items by name. It never writes and never creates Requests.
type Searcher interface {
    Name() string
    MediaTypes() []domain.MediaType
    Search(ctx context.Context, q SearchQuery) (SearchPage, error)
}

type SearchQuery struct { Text string; MediaType domain.MediaType; Page int; Limit int }

type SearchResult struct {
    MediaType   domain.MediaType
    Provider    string // "tmdb" | "musicbrainz" | "igdb" | "audible"
    ProviderID  string
    Ref         string // canonical URL or bare ASIN that Service.Resolve accepts
    Title       string
    Year        int
    Subtitle    string // artist, author/narrator, platform hint
    ArtworkURL  string
    ArtworkKind domain.ArtworkKind
    Score       float64
}
```

Each provider package (`tmdb`, `musicbrainz`, `igdb`, `audible`) implements `Searcher` next to its `Resolver` and owns its DTOs, as the existing package READMEs require (`internal/providers/metadata/tmdb/README.md`). A `SearchService` fans out, caches and annotates. A `Searcher` is **not** a `Resolver`, so provider code that only searches cannot create catalog rows.

### 3.2 Mapping to the canonical item (the key invariant)

`SearchResult` is *not* a `domain.CatalogItem`. It is a pointer to one. The invariant: **`Ref` must be a value that the same provider's `Resolver` already accepts**, so `POST /resolve` produces the identical `domain.ResolvedItem` as if the person had pasted the link. Concretely:

| Media type | `Ref` | Existing acceptor |
| --- | --- | --- |
| movie, tv | `https://www.themoviedb.org/movie/<id>` / `/tv/<id>` | TMDB resolver (`tmdb/resolver.go`); movie and TV IDs are separate namespaces (`docs/REQUESTS_API.md`) |
| album | `https://musicbrainz.org/release-group/<mbid>` | MusicBrainz resolver (D020) |
| song | `https://musicbrainz.org/recording/<mbid>` | MusicBrainz resolver (D020) |
| game | `https://www.igdb.com/games/<slug>` | IGDB resolver (`igdb/resolver.go:180`) |
| audiobook | `https://www.audible.<tld>/pd/<ASIN>` or bare ASIN | Audible `Canonicalize` (`audible/resolver.go`) |

A table-driven test per provider should assert `Resolver.CanResolve(parse(result.Ref))` for every fixture result. That test is the contract that keeps D002's promise.

Consequence to accept: selecting a result costs one more provider call (the detail fetch, 9 s budget, `docs/REQUESTS_API.md`), as pasting does today. Do not try to upsert search rows into the catalog directly: a sparse row would skip the typed details, selection lists (D027) and platform list (D009) that the modal depends on. Do not trust any search-supplied metadata at request time: `POST /requests` keeps taking `url` or `catalogId` only (`internal/httpapi/router.go`, `POST /requests`), the lesson from GG Requestz.

### 3.3 HTTP endpoint (draft)

`GET /api/v1/search`, `accessSession` like `POST /resolve` (`internal/httpapi/router.go:383`), added to `internal/httpapi/access.go` and to `api/openapi.yaml` (the contract test keeps the two in step). It is a GET with no side effects, so no CSRF requirement beyond what the route table applies to reads. Viewers may search (they can already resolve); `canRequest` stays on the preview.

```yaml
/search:
  get:
    operationId: searchCatalog
    summary: Search providers by name; results point at items that /resolve can open
    parameters:
      - { in: query, name: q, required: true, schema: { type: string, minLength: 2, maxLength: 200 } }
      - { in: query, name: mediaType, required: true, schema: { type: string, enum: [movie, tv, movie_tv, album, song, game, audiobook] } }
      - { in: query, name: page, schema: { type: integer, minimum: 1, maximum: 10, default: 1 } }
    responses:
      '200':
        description: One page of results; Cache-Control no-store
        content:
          application/json:
            schema:
              type: object
              required: [results, page, hasMore]
              properties:
                page: { type: integer }
                hasMore: { type: boolean }
                results:
                  type: array
                  maxItems: 50
                  items:
                    type: object
                    required: [mediaType, provider, providerId, ref, title]
                    properties:
                      mediaType: { type: string }
                      provider: { type: string }
                      providerId: { type: string }
                      ref: { type: string, description: "Pass to POST /resolve as url" }
                      title: { type: string }
                      year: { type: integer }
                      subtitle: { type: string }
                      artworkUrl: { type: string }
                      artworkKind: { type: string, enum: [poster, square] }
                      local:
                        type: object
                        description: Local state only; absent when the item has never been resolved
                        properties:
                          catalogId: { type: string, format: uuid }
                          requestStatus: { type: string, enum: [requested, approved, denied, available, cancelled] }
                          availability: { type: string, enum: [unknown, available, missing] }
      '400': { description: Invalid q, mediaType or page (invalid_search) }
      '401': { description: Authentication required }
      '502': { description: Provider rejected Requestarr's credentials (metadata_provider_credentials) }
      '503': { description: Provider busy (metadata_provider_busy, Retry-After) or not configured (metadata_provider_not_configured) }
```

Notes: reuse `writeResolveError` mapping (`internal/httpapi/resolve_errors.go`) and add one fallback code. `q` limits mirror `GET /api/v1/requests` (`docs/REQUESTS_API.md`: 200 characters, control characters rejected, trimmed). Pagination: TMDB and Audible give pages; MusicBrainz and IGDB give `offset`/`limit`; expose a simple `page` and map it per provider (offset = (page-1)*limit). No snapshot cursors are needed; unlike the Requests queue, search results are not a stable set and paging past about 10 pages is not a real use. `hasMore` avoids promising `totalResults` that MusicBrainz's score-ordered `count` and IGDB (no count without a second query) cannot cheaply give.

`mediaType=movie_tv` maps to TMDB `search/multi` (movie and TV only) and is the one blended type. Each other type maps to exactly one provider call. A blended "all" type is deliberately not offered in the first phases (3.5).

### 3.4 Dedup against existing Requests and library

The invariant is one shared Request per item (D003, AGENTS.md rule 2), so the search list should *show* the existing state rather than hide the row (Seerr shows status; DroppedNeedle shows `in_library`; BookOrbit batch-checks).

- Annotate with **one batched local query** over `(provider, provider_id, media_type)` for the page's results (at most 50, the cap BookOrbit also uses), joining `catalog_items`, the open/closed Request, and availability. Read the same local state `GET /catalog/{id}/preview` serves (`internal/httpapi/movie_preview.go`); no provider or library calls (AGENTS.md rule 10, `docs/ARCHITECTURE.md` "Fast-path principle").
- Rows never resolved before have no catalog row, so `local` is absent and means "unknown", not "missing" (the preview already says unknown availability is not shown as missing, `docs/REQUESTS_UI.md`). Whether library inventory is keyed so it can be matched by provider ID *before* a catalog row exists could not be determined from the docs read; check `internal/store/postgres/inventory.go` (D014) when implementing. If it is not, the "In library" badge appears only for items someone has previously previewed, and the modal still resolves the truth.
- The badge is advisory; the transactional writes still enforce everything (`docs/REQUESTS_API.md` says the same of preview flags). Selecting an already-requested row opens the same modal, which already offers Notify Me, View request and Add to request (D027).
- Games: IGDB game identity is per game, platform handling is in the modal (D009, D021). The search row shows no platform availability.
- Audiobooks: two ASINs of one book are two Requests (D031). The search list will show several editions of the same title; the row must show narrator, runtime and abridged/unabridged so the choice is meaningful. This is a limitation to state in the UI copy, not to fix in search.
- Hiding rows that are already requested or available (a Seerr option) is not recommended: it makes a successful earlier request look like a missing search result.

### 3.5 Per-type selector, or multi-search

- Video: one `search/multi` call genuinely returns movies and TV together, as Seerr does, so "Movies & TV" is a natural single tab.
- Music, games and audiobooks come from three different providers with very different budgets (MusicBrainz 1 per second per IP, IGDB 4 per second and 8 in flight, Audible undocumented). A blended "All" would fan out 4 provider calls per keystroke burst and would stall behind the shared MusicBrainz spacer.
- Recommendation: a segmented control (Movies & TV, Albums, Songs, Games, Audiobooks), persisted locally and defaulted to the last used, and hidden for any provider that is not configured. An "All" tab is a later option; if added it must run on explicit submit only and show per-provider status (BookOrbit streams per-provider results with status; DroppedNeedle returns buckets with a status field).
- Albums and Songs as separate tabs match D020 (release group vs recording) and MusicBrainz's separate indexes.

### 3.6 Debounce, typeahead and the request budget

- Debounce 300 ms (Seerr, GG Requestz) to 350 ms (BookOrbit hint); minimum 2 characters (DroppedNeedle, GG Requestz). Cancel the previous request with `AbortController`, and ignore any response whose generation is not current (BookOrbit's generation counter, Listenarr's search id).
- Per-provider policy: typeahead for TMDB and IGDB; **explicit submit (Enter or button)** for MusicBrainz and Audible. The MusicBrainz budget is a hard 1 per second for the whole instance and also feeds previews, tracklists and alias lookups, and a household of several people would otherwise starve the resolver. Audible is undocumented.
- Server side: the existing `spacer` already queues MusicBrainz calls; give search a lower priority than a resolve (a person clicking a result is waiting), a short queue depth and context cancellation so a cancelled browser request frees its slot. DroppedNeedle uses a priority token bucket for the same reason. IGDB: a small semaphore (for example 4 concurrent, under the documented 8 open), 429 mapped to `ErrBusy` like the existing resolvers. TMDB: honour 429 with `ErrBusy`.
- Single-flight identical concurrent queries so five people typing "dune" make one provider call.
- A per-user limit on `GET /search` (a modest token bucket) protects the shared provider budgets from one client; `internal/httpapi` has no general rate limiter today (only `issues.ErrRateLimited`), so this is new code.
- Results are not needed before the person has seen their own page: do not fetch cover art or details inline (DroppedNeedle prefetches covers in the background). Artwork URLs already point at provider CDNs (TMDB image CDN, `coverartarchive.org`, `images.igdb.com`); a search row can carry a CDN URL without an extra call.

### 3.7 Caching, failure and security

- Cache: in-process, keyed by `(provider, mediaType, normalized text, page, language/region)`; **fresh 60-90 s, stale up to 6 hours, used only when the provider is busy or down** (DroppedNeedle's 90 s / 6 h; Requestarr's resolver already serves stale only on `ErrBusy`, `provider.go`). Bounded size (the resolver cache holds 256 entries). Do not persist search results in PostgreSQL: they are not catalog records, and TMDB limits retention to six months in any case; only resolved items are upserted, as today.
- Failure: never show a provider failure as "No results" (Seerr's wrapper swallows errors into an empty page; GG Requestz returns `[]`). Return 503 with `metadata_provider_busy` / `_not_configured` and show an inline message with "You can still paste a link", so D002's path remains the always-available fallback.
- Partial support: unconfigured providers should hide their tab rather than fail (the Setup wizard configures TMDB token and IGDB client, `docs/FIRST_RUN.md`; this mirrors `ErrNotConfigured`).
- Security: treat `q` as untrusted (AGENTS.md); length and control-character validation as for `GET /requests`; URL-encode for TMDB and MusicBrainz; MusicBrainz Lucene special characters must be escaped (or `dismax=true` used) so a typed `*`, `:` or `"` does not change the query or raise errors; escape Apicalypse strings for IGDB. Hosts are fixed per provider, so there is no SSRF surface unless a mirror setting is added. Response size and timeouts follow the existing `metadata.FetchRetrying` helper (`internal/providers/metadata/http.go`).
- Do not log queries at info level in a way that profiles users; the request ID is enough (AGENTS.md logging rules cover secrets, not queries, so this is a judgment call).

### 3.8 Accessibility and UX

- One labelled input (`<label>` or `sr-only` text as the existing paste box has), `autoComplete="off"`, `spellCheck={false}` off for names, an always-present Clear button, and a type selector as a real radio group or tabs with arrow-key support.
- Results as a list of `<button>` rows in a region with `aria-live="polite"` status text ("12 results for dune", "Searching...", "Search unavailable"). A combobox with `aria-activedescendant` (DroppedNeedle's choice) is the right ARIA pattern only for a dropdown; for a result list below the field a plain list is simpler and more robust. If a dropdown is later built for typeahead, copy the combobox/listbox roles listed above, not the implementation.
- Status shown with text and icon, not colour alone (AGENTS.md accessibility precedence, `docs/ACCESSIBILITY_AUDIT.md`); artwork keeps its native shape (D010: posters 2:3, music and audiobook square).
- Opening a result reuses the modal and its focus-return behaviour (`docs/REQUESTS_UI.md`: Cancel/Close and Escape return focus to the initiating control). The initiating control is now the result row, so the list must stay mounted, with its scroll position, while the modal is open.
- Empty, loading and error states are distinct; Back should restore the query (Seerr keeps the query in the URL, `useSearchInput.ts`). Put `?q=&type=` in the Home URL so searches are bookmarkable, consistent with "Keep URLs/bookmarkable filters" (AGENTS.md).
- Keep the paste path visible; do not make search the only affordance. The supported-links footer on Home (`web/src/pages/HomePage.tsx`) can become a supported-sources list.

## 4. Decisions and documents affected

| Item | Conflict | Proposed amendment |
| --- | --- | --- |
| D002 "Link-first V1" | Says users paste links; discovery "can be added later without changing the request modal/domain path". | Not reversed. Add a new decision (next free number after D031) "Name search is a front door to the resolver" recording: search returns resolver-acceptable refs, no new request path, search never writes. D002 stays accepted for V1; the new record supersedes only its "no search" part for the phased types. |
| `docs/V1_SCOPE.md`, "Explicitly out of V1: Native catalog search/discovery/trending/recommendations" | Directly in conflict for *search*. | Split the line: move "name search" in or to a "V1.x" list, and keep "discovery/trending/recommendations" out. |
| `AGENTS.md` accepted decision 1 "Link-first request entry using TMDB, MusicBrainz, IGDB and Audible URLs" | Binding agent rule; implies link only. | Reword after the decision is accepted (agents must not widen scope silently). Search does not touch the acquisition boundary. |
| `CLAUDE.md` / `docs/PROJECT_VISION.md` | CLAUDE.md summary calls V1 "link-first"; `docs/PROJECT_VISION.md` was not read in detail here. | Update wording once decided. |
| `docs/ARCHITECTURE.md` "Fast-path principle": "External API calls belong in URL resolution, explicit connection tests, and background refresh/sync jobs." | Search is a fourth, interactive caller of providers. | Add search to that list, with the cache/budget rules in 3.6-3.7. Page rendering remains local-only (AGENTS.md rule 10 is about routine UI rendering; a user-typed search is an explicit action like resolve). |
| `docs/ARCHITECTURE.md` module list: `providers/metadata` "turns a pasted URL into a canonical catalog item" | Description becomes incomplete. | Add "and finds candidate items by name". |
| `api/openapi.yaml` and `internal/httpapi/access.go` | New route. | Add; the contract test enforces both. |
| TMDB attribution (TMDB API terms §3) | More TMDB content on more screens. | Verify and, if absent, add the logo and non-endorsement notice before shipping search. |
| D020 / D031 | None. | Note that MusicBrainz budget is shared; audiobook search uses the undocumented Audible catalog under the same caveats as D031. |

No conflict with D001 (acquisition), D003 (one shared request), D007 (library authority) or D008 (local state): search does not alter any of them.

## 5. Recommendation and phasing

**Recommended: add name search as a thin, read-only front door to the existing resolver, phased by provider risk.**

1. **Phase 0, decisions.** Record the new decision and split the V1 scope line (section 4). Owner chooses the open questions below.
2. **Phase 1, Movies & TV (TMDB).** `Searcher` interface, `SearchService` (fan-out of one, cache, single-flight, local annotation), `GET /search`, the Home "Search or paste a link" box, type selector with one type, accessibility pass, fixture tests including the `CanResolve(ref)` contract. Lowest risk: the best-behaved provider (about 40 per second, 429, documented), the richest result data, and the same response Seerr proves out.
3. **Phase 2, Games (IGDB).** Add escaping, `offset` paging, the game kind filter, slug refs. 4 per second with 429.
4. **Phase 3, Music (MusicBrainz).** Albums then Songs, explicit-submit only, lower priority than resolve in the shared spacer, Lucene escaping, hide-non-album default. The most operationally delicate: the 1 per second budget is shared.
5. **Phase 4, Audiobooks (Audible catalog).** Explicit submit, admin region, multi-part labels. Last because the endpoint is undocumented and its terms were unreadable (audiobook research).
6. **Later, optional.** "All" tab with per-provider status; a typeahead dropdown; recent searches; query-to-ASIN helpers; any discovery rows (a separate decision).

Trade-offs:

- *Refs through `/resolve` (recommended) versus upserting search rows as catalog items.* The first costs one extra provider call per click but keeps the modal, selections (D027), platforms (D009) and cache untouched and cannot create sparse catalog rows. The second is faster to display but forks the domain path D002 promises not to fork.
- *Per-type tabs versus blended search.* Tabs respect provider budgets and the type-specific identities (D020, D031); a blended list is friendlier but multiplies calls and hides MusicBrainz/Audible failures. Start with tabs.
- *Typeahead versus explicit submit.* Typeahead is nicer but only safe on TMDB and IGDB; MusicBrainz and Audible need explicit submit or a long debounce plus cancellation.
- *Show existing state versus hide it.* Showing keeps the one-shared-request rule visible and avoids "where did it go"; hiding is a possible later setting.
- *No persisted search cache.* Simpler and ToS-friendly; costs repeated provider calls after restarts, acceptable at household scale.

## Open questions for the owner

Status after D032: 1 settled (V1.x); 7 settled in part (non-commercial assumption and hotlinking recorded; TMDB attribution still to confirm); 12 settled (hotlinking accepted); 8 and 10 still open (terms unread); 2-6, 9 and 11 are answered by the spec and tickets in `.scratch/name-search/`, not by D032.

1. Scope and timing: is name search a V1 amendment or V1.x? D002, `docs/V1_SCOPE.md` and AGENTS.md all say link-first; a dated decision is required either way.
2. Which media types first? The recommendation is Movies & TV, then games, music, audiobooks. Is audiobook search wanted at all given the undocumented Audible endpoint?
3. Should "Search or paste a link" replace the current paste box on Home, or should search live on its own page or in the main navigation?
4. Is a blended "All" type needed in the first release? (Recommendation: no.)
5. Should already-requested or available items be hidden, flagged, or optionally filterable? (Recommendation: flagged.)
6. Which albums should a search show: albums only, or also singles/EPs/compilations (MusicBrainz `primarytype`)? Same for IGDB game kinds (remakes, remasters, ports, DLC), which first needs the live `category`/`game_type` field verified.
7. TMDB attribution: is the logo and non-endorsement notice already shown anywhere? It is required by https://www.themoviedb.org/api-terms-of-use (§3) and becomes more visible with search. Also: does Requestarr's use count as non-commercial for TMDB, MusicBrainz and IGDB (self-hosted, free, AGPL)? Only the operator/maintainer can answer.
8. Does the Twitch Developer Service Agreement (IGDB) restrict caching search responses? Not read; could not be determined.
9. Can library inventory be matched to search results by provider ID before a catalog row exists (for the "In library" badge on never-previewed items)? Check `internal/store/postgres/inventory.go`; otherwise the badge is limited to previously previewed items.
10. Does the Cover Art Archive have its own rate or usage terms that matter once search shows many album covers at once? Not read; the current resolver already hotlinks `front-500` URLs.
11. Per-user search rate limit values and whether an admin can disable search per provider (to protect a shared MusicBrainz IP budget).
12. Is the owner comfortable with search results in the browser hotlinking provider CDNs (TMDB, Cover Art Archive, IGDB images) as previews already do? (Privacy consideration: provider CDNs see viewers' IPs.)

## Source index

- Seerr: https://github.com/seerr-team/seerr (`server/routes/search.ts`, `server/lib/search.ts`, `server/api/themoviedb/index.ts`, `server/entity/Media.ts`, `server/constants/media.ts`, `server/routes/discover.ts`, `server/routes/request.ts`, `src/hooks/useDebouncedState.ts`, `src/hooks/useSearchInput.ts`, `src/components/Layout/SearchInput/index.tsx`, `src/components/Search/index.tsx`, `seerr-api.yml`). MIT.
- DroppedNeedle: https://github.com/DroppedNeedle/DroppedNeedle (`backend/api/v1/routes/search.py`, `backend/services/search_service.py`, `backend/repositories/musicbrainz_base.py`, `frontend/src/lib/components/SearchSuggestions.svelte`). AGPL-3.0.
- GG Requestz: https://github.com/XTREEMMAK/ggrequestz (`src/lib/igdb.js`, `src/lib/gameCache.js`, `src/lib/cache.js`, `src/routes/api/search/+server.js`, `src/routes/api/request/+server.js`, `src/components/SearchBar.svelte`). GPL-3.0.
- BookOrbit: https://github.com/bookorbit/bookorbit (`client/src/features/book-requests/components/RequestSearchPanel.vue`, `client/src/features/book/composables/useMetadataSearch.ts`, `server/src/modules/book-request/*`). AGPL-3.0.
- Listenarr: https://github.com/Listenarrs/Listenarr (`listenarr.api/Features/Search/SearchController.cs`, `listenarr.application/Metadata/Audible/AudibleProductSearchWorkflow.cs`, `fe/src/stores/addNewSearch.ts`). AGPL-3.0.
- TMDB: https://developer.themoviedb.org/reference/search-multi, https://developer.themoviedb.org/reference/search-movie, https://developer.themoviedb.org/reference/search-tv, https://developer.themoviedb.org/docs/rate-limiting, https://developer.themoviedb.org/docs/image-basics, https://www.themoviedb.org/api-terms-of-use.
- MusicBrainz: https://musicbrainz.org/doc/MusicBrainz_API, https://musicbrainz.org/doc/MusicBrainz_API/Search, https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting, https://musicbrainz.org/doc/About/Data_License; live probe of `/ws/2/release-group` on 2026-11-04.
- IGDB: https://api-docs.igdb.com/ (Getting Started, Authentication, Rate Limits, Images, Search, Pagination, Multi-Query, enum migration notice).
- Audible catalog: live probe of `https://api.audible.com/1.0/catalog/products/` on 2026-11-04; `docs/research/AUDIOBOOK_INTEGRATION.md`.
- Requestarr: `docs/DECISIONS.md` (D001-D031), `docs/V1_SCOPE.md`, `docs/ARCHITECTURE.md`, `docs/REQUESTS_API.md`, `docs/REQUESTS_UI.md`, `AGENTS.md`, `internal/providers/metadata/*`, `internal/httpapi/router.go`, `internal/httpapi/resolve_errors.go`, `api/openapi.yaml`, `web/src/pages/HomePage.tsx`.
