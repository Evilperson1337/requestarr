# Audiobook integration research

Recorded 2026-11-02. Question: how viable is an **audiobook** media type where, for v1, the requester supplies an ASIN and availability comes from syncing Audiobookshelf (ABS) or BookOrbit?

Method: primary sources only. Source code was read from shallow clones of the official repositories at these revisions: ABS `advplyr/audiobookshelf` `ce41ce4` (package.json version 2.37.2, 2026-11-01), BookOrbit `bookorbit/bookorbit` `76450d6` (2026-11-01), Audnexus `laxamentumtech/audnexus` `develop` (2026-10-28). File references below are `path:line` at those revisions, with the repository named. Live HTTP calls to Audnexus, Audible and AudiobookDB's published docs were made on 2026-11-02 and are labelled "observed". A second pass on 2026-11-02 closed the earlier "Unverified" items; anything still undetermined says "Could not be determined" and why.

## Verdict

**Viable, with a narrow v1.** The architecture fits: `inventory.Source` (`internal/inventory/source.go`) already takes a `MediaType` and `ExternalIDs`, and a stable external ID (the ASIN) exists on both sides of the match. The honest limits are:

1. **ABS is a good first Source.** It has a documented-in-code API-key mechanism, an `asin` field on every book, and cheap full enumeration. Recommend ABS only for v1.
2. **BookOrbit is a real, official project, but it is a poor fit today.** It has no machine API token (only user session JWTs, plus a separate Basic-auth OPDS login that carries no ASIN), no published API docs (Swagger is off by default), and its list/query rows (`BookCard`) **do not include** `audibleId`/`amazonId`; the filter cannot select on them either, so the ASIN is only reachable through one `GET /books/:id` call per book. It also ships its own request and download-client features (acquisition), which overlaps what Requestarr must not do. Defer.
3. **ASIN to metadata is the weakest link.** There is no documented, contractual first-party API. Audible's catalog endpoint works but is undocumented and its terms say nothing either way; Audnexus is a free community API with no stated terms (observed rate limit 300 per 60 s) and a declared successor; the successor (AudiobookDB) requires a key, attribution and a **no-long-term-caching** term, and its ASINs are release-scoped. ASINs are also **region-scoped** (observed below). V1 must treat the ASIN as the identity and metadata as best-effort display data.
4. **Matching on ASIN will miss some library items** (ASIN absent, different edition or region). Needs the existing reviewable title-candidate path, plus a manual-confirm path.

## 1. Audiobookshelf

### Auth

- ABS issues **API keys** as JWTs of `type: 'api'`, validated against an `apiKeys` table (active flag, optional expiry, tied to a user): `server/auth/TokenManager.js:278-300` (audiobookshelf).
- Key management endpoints (`GET/POST/PATCH/DELETE /api/api-keys`) are at `server/routers/ApiRouter.js:338-341`. Creation requires `name`, a `userId`, and optional `expiresIn` (`server/controllers/ApiKeyController.js`, create handler).
- The JWT strategy reads the token from the `Authorization: Bearer` header **or** a `?token=` query parameter: `server/Auth.js:124-131`. Requestarr should use the header only; query-string tokens leak into logs.
- API keys exist in tag `v2.26.0` and not in `v2.25.0` (`git show <tag>:server/routers/ApiRouter.js`, checked locally). Older servers would need a user JWT from `/login`; **recommend requiring >= 2.26.0**. Only admin-or-up users can create, list or delete keys (`ApiKeyController.js:197-203`, `middleware`); a key can be created for a non-root user (`ApiKeyController.js`, create handler).
- Permission model (verified): a key acts as its user. Account types are `admin`, `user`, `guest` (`server/models/User.js:120`). Default permissions for a plain `user` are `update: false`, `delete: false`, `upload: false`, `createEreader: false`, `download: true`, `accessAllLibraries: true` (`User.js:169-183`), and the admin-only operations (scan, library edit) are gated on `isAdminOrUp` (e.g. `LibraryController.js:51,256,526`). So **a dedicated plain `user` account, optionally with `librariesAccessible` limited, is read-only for enumeration** in the sense that matters (it cannot edit metadata or scan); there is no separate "read-only" role and it keeps `download: true` (disable it on the account to remove file downloads). `guest` is further restricted for progress/bookmarks only (`MeController.js:41,81,439`); it is not needed. Earlier text in this document that said a standard user can edit metadata was wrong.

### Enumerating libraries and items

- `GET /api/libraries` and `GET /api/libraries/:id` (`ApiRouter.js:70-71`). A library has a read-only `mediaType` of `book` or `podcast` (`docs/objects/Library.yaml:91-94`, column `server/models/Library.js:165`); only `book` libraries matter. Each library item also carries its own `mediaType` (`server/models/LibraryItem.js:1001-1030`, `toOldJSONMinified`).
- `GET /api/libraries/:id/items` (`ApiRouter.js:75`). Query params read at `server/controllers/LibraryController.js:605-624`: `limit` (default 0 = **all items**), `page`, `sort`, `desc`, `filter`, `minified`, `collapseseries`, `include`. So pagination is `limit`/`page` (0-based); `limit=0` returns everything in one response.
- Per-item identifiers: `media.metadata.asin` and `media.metadata.isbn` are returned by the minified and expanded forms (`oldMetadataToJSONMinified`, `server/models/Book.js:~580-595`; `abridged`, `narratorName`, `authorName`, `language`, `publishedYear` are in the same object). Both are also in the OpenAPI schema at `docs/objects/metadata/BookMetadata.yaml:68` (audiobookshelf).
- Item-level fields for change detection: `addedAt` and `updatedAt` (ms epoch), `isMissing`, `isInvalid` (`server/models/LibraryItem.js:982-987`). `isMissing` is ABS's own "files gone" flag, which Requestarr should treat as **absent** for availability.
- Audio versus ebook-only (verified): the minified book object (`server/models/Book.js:643-663`, `toOldJSONMinified`) is `{id, metadata, coverPath, tags, numTracks, numAudioFiles, numChapters, duration, size, ebookFormat}` where `numTracks = includedAudioFiles.length`, `numAudioFiles = audioFiles.length`, `ebookFormat = ebookFile?.ebookFormat`. The spec describes `ebookFormat` as "Will be null if the book is an audiobook" and also lists `numMissingParts`, `numInvalidAudioFiles` (`docs/objects/mediaTypes/Book.yaml:43-68`). **Rule for the adapter: treat an item as an audiobook only when `media.numTracks > 0` (use `numAudioFiles > 0` only as a cross-check); ebook-only items have `numTracks == 0` and a non-null `ebookFormat`; an item with both counts as audiobook.** `duration` (seconds) is a further sanity check. The exact wire shape should still be pinned by a recorded fixture because `numMissingParts`/`numInvalidAudioFiles` are documented but not emitted by the minified method above (spec drift, see Stability).

### Lookup/filter by ASIN

- There is **no filter that selects by ASIN value.** The `filter` groups are progress, series, abridged, explicit, genres/tags/narrators, publishers, languages, tracks, ebooks, missing, publishedDecades (`libraryItemsBookFilters.js:109-236`). `filter=missing.asin` finds items **lacking** an ASIN (`:220-221`), which is useful for diagnostics.
- `GET /api/libraries/:id/search?q=` matches `asin` and `isbn` by **substring** (`libraryItemsBookFilters.js:1101-1118`). Usable for an ad-hoc lookup, not for exact matching.
- Conclusion: enumerate once (`limit=0` or paged) and build a local ASIN map, which is exactly what `inventory.Source.Snapshot` does.

### Change detection

- Socket.IO events: `item_added` (`server/scanner/LibraryScanner.js:648`), `item_updated` (`:598`, `Scanner.js:129`), `items_updated` (`LibraryScanner.js:230,238`), `item_removed` (`ApiRouter.js:403`). The socket requires an `auth` event carrying the token (`server/SocketAuthority.js:188`).
- Recommendation: do **not** use sockets in v1. Requestarr's contract is a complete periodic snapshot (a source failure must never imply removal). A long-lived socket adds a second state machine with no guarantee of completeness. `updatedAt` / `addedAt` can be recorded as evidence only (`Item.AddedAt` already exists).
- `POST /api/libraries/:id/scan` triggers an ABS scan (`ApiRouter.js:91`); Requestarr should not call it (read-only, no side effects on the source).

### Audible metadata match and the provider endpoints

- `GET /api/search/books?provider=audible&title=&author=&id=` (`ApiRouter.js:287`; handler `server/controllers/SearchController.js:85-105`). Providers include `audible`, `audible.ca/.uk/.au/.fr/.de/.jp/.it/.in/.es`, plus google, itunes, openlibrary, fantlab, audiobookcovers (`server/finders/BookFinder.js:26`).
- ABS resolves an ASIN by calling **Audnexus** (`https://api.audnex.us/books/<ASIN>?region=<r>`, `server/providers/Audible.js:~100-125`), and falls back to Audible's own catalog search (`https://api.audible<tld>/1.0/catalog/products`, `Audible.js:156`) for title queries. Region to TLD map: us .com, ca .ca, uk .co.uk, au .com.au, fr .fr, de .de, jp .co.jp, it .it, in .in, es .es (`Audible.js:9-20`).
- Implication: ABS's own "match" uses the same two upstreams recommended in section 3, so using ABS as a *resolver* adds an ABS dependency for no gain. Use Audnexus/Audible directly.
- ABS's search is an authenticated endpoint on the user's server; there is no reason for Requestarr to proxy through it.

### Stability and documentation

- ABS ships an OpenAPI spec in-repo (`docs/README.md`, `docs/openapi.json`, e.g. `/api/libraries/{id}` at `docs/openapi.json:637`). **It is a lint-checked description, not a maintained contract.** `.github/workflows/lint-openapi.yml` only runs `redocly lint` on `docs/root.yaml` and `docs/openapi.json`. `git log` shows `docs/openapi.json` last changed 2024-08-18 (`27b3a441`) and `docs/root.yaml` 2024-07-31 (`1e6dd0e3`), while the code is at 2.37.2; `openapi.json` contains no `api-keys` route, although `ApiRouter.js:338-341` serves them (added in 2.26.0), and the spec lists `numMissingParts` while the minified method does not emit it. The README (`docs/README.md`) makes no stability or versioning promise.
- The API is the one ABS's own web UI and mobile apps use, with no `/v1` prefix, so path stability rests on those clients. Treat the response shape as untrusted (AGENTS.md rule), tolerate missing fields, and pin behaviour with recorded fixtures, not the spec.
- License (verified): GPL-3.0, `LICENSE` header "GNU GENERAL PUBLIC LICENSE Version 3, 29 June 2007" (audiobookshelf). Requestarr only calls its HTTP API, which does not copy code.

## 2. BookOrbit

### What it is (verified)

- **BookOrbit** is a self-hosted library and reading platform for ebooks, PDFs, audiobooks and comics. Official repo: https://github.com/bookorbit/bookorbit (README: "A self-hosted library and reading platform for ebooks, PDFs, audiobooks, and comics"); site https://bookorbit.app; AGPL-3.0 (`server/package.json` `"license": "AGPL-3.0-only"`). NestJS + PostgreSQL server, native iOS app, Android app in closed testing (README).
- It is **not** the "Book Orbit" of any other project; the name resolved unambiguously to this repository. It is the only project of that name found with an official repository, website and docs; whether it is the one the requester means cannot be determined from sources and should be confirmed with them. It is young and fast-moving (README: app features need "BookOrbit v3.0.0 or later").

### API and auth

- The server uses a global prefix `api/v1` (`server/src/main.ts:70`).
- Auth is **user sessions**: local login/refresh/logout, OIDC, magic links (`server/src/modules/auth/auth.controller.ts:55-200`), with an HS256 JWT read from the bearer header or an `access_token` cookie (`server/src/modules/auth/jwt.strategy.ts`). A search of `server/src` for API key / personal access token / service account found only third-party provider keys (Hardcover, ComicVine, Readwise, TTS), **no machine API token for the BookOrbit API itself**. Requestarr would need to hold a user's credentials and manage refresh tokens, or rely on OIDC. That is a large integration cost and a security smell (a stored user password). Other credential paths found (verified): (a) **OPDS** uses HTTP Basic auth against separate OPDS users with their own password hash (`server/src/modules/opds/opds-auth.guard.ts:71-114`, `opds-user.service.ts:44-51`); the OPDS service and XML helpers emit no ASIN/Audible/ISBN identifiers (grep of `opds.service.ts`, `opds-xml.helpers.ts`, `opds-book.service.ts` found only `urn:bookorbit:*` ids), so OPDS cannot supply an ASIN. (b) **Kobo** uses a per-device token in the URL path (`@Controller('kobo/:deviceToken')`, `modules/kobo/kobo-auth.controller.ts:17`), scoped to the Kobo sync protocol, not the JSON API. No personal-access-token or API-key flow for the main API exists.
- API docs: the OpenAPI/Swagger UI is **disabled by default** (`SWAGGER_ENABLED=false`, `docs/DEVELOPMENT.md:193-204,242`); the website mentions no public REST API or API tokens (https://bookorbit.app). So there is no documented, supported external API contract.

### Identifiers and enumeration

- The metadata schema has `isbn10`, `isbn13`, `amazonId`, `audibleId`, `goodreadsId`, `hardcoverId`, `abridged`, `durationSeconds` (`server/src/db/schema/metadata.ts:49-75`). `audibleId` is `varchar(20)` and the DTO normalises it to upper-case (`server/src/modules/book/dto/book-dto-validation.test.ts:275-288`). The built-in Audnexus metadata provider writes it (`server/src/modules/metadata-fetch/providers/audnexus/audnexus.provider.ts`). Its Audible id is **an ASIN**, but note two separate columns (`amazonId` and `audibleId`), so a library may hold the ASIN in either.
- Enumeration: `POST /api/v1/books/query` (`server/src/modules/book/book.controller.ts:153`) with `pagination {page (0-based), size}`, a filter tree and sort, and a cap on the page window (`book-query.pipe.ts:27-39`). `GET /api/v1/books/search` (`:148`) is free text. `GET /api/v1/books/:id` returns `BookDetail`, which includes `providerIds` (`packages/types/src/book.ts:304-345`).
- Verified: `POST /books/query` returns `BooksPage {items: BookCard[], total, page, size}` (`packages/types/src/book.ts:453-458`). `BookCard` (`book.ts:195-232`) has `isbn13`, `hardcoverId`, `hardcoverEditionId`, `narrators`, `files[]`, `addedAt`, `updatedAt`, but **no `audibleId`, `amazonId` or `providerIds`**. Those exist only on `BookDetail` (`providerIds`, `book.ts:337`). The filter rule fields (`StaticRuleField`, `packages/types/src/query.ts`) include `title`, `isbn` (operators `eq`, `isEmpty`, `isNotEmpty`), `format`, `addedAt`, `fileAvailability` but **no audible/amazon/provider-id field**. So an ASIN snapshot costs 1 query pass plus **one `GET /books/:id` per audiobook** (N+1), and ASIN cannot be looked up server-side. Matching on `isbn` (via `eq`) is possible only if the ABS-style ISBN is present, which Audible audiobooks often lack.
- Change events (verified): a Socket.IO `/scan` namespace emits `scan:progress`, `scan:books:added`, `book:missing`, `book:restored`, `book:moved`, `book:transferred` to `library:<id>` rooms, joined by an authenticated user session via `subscribe:library` (`server/src/modules/scanner/scan.gateway.ts:33,110-183`). That is a user-session push channel with no payload guarantee of completeness; not usable as a source of truth for Requestarr's complete-snapshot rule.

### Overlap with the product boundary

BookOrbit has its own `book-requests`, `request-indexers`, `download-clients` and `book-request-fulfilment` modules (`server/src/modules/book-request/*`, controllers at `book-request.controller.ts:17`, `admin/request-indexers`, `admin/download-clients`). Requestarr only reads library contents, but an admin running BookOrbit with those enabled has a second request system for the same items; docs should say so, and Requestarr must never call those endpoints (AGENTS.md product boundary).

## 3. Resolving an ASIN to metadata

### Options and their standing

| Option | Status | Notes |
| --- | --- | --- |
| Audible catalog API `https://api.audible.<tld>/1.0/catalog/products/<ASIN>?response_groups=...` | **Works (observed 200, 2026-11-02) but not documented or offered as a public API by Audible.** | Used by Audnexus (`src/helpers/books/audible/ApiHelper.ts:56-68`) and ABS (`Audible.js:156`). No terms, key, or rate limit published. Terms: the Audible Conditions of Use page (https://www.audible.com/legal/conditions-of-use) contains no clause on automated access, scraping or APIs; it bars using "the Service or Content for any commercial or illegal purpose" and incorporates the Amazon.com Conditions of Use and the Audible License by reference. Those two documents could not be read (amazon.com returned HTTP 503 twice), so whether they restrict scripted catalog calls **could not be determined**. A search result for the Audible for Business terms shows a "no right to data mine or gather data" clause, but that agreement binds business customers and was not fetched in full. Treat the endpoint as tolerated, not licensed. Cover and narrator come from `response_groups` (`contributors`, `media`, `product_attrs`, `product_desc`, `series`, `image_sizes`). |
| Audnexus `https://api.audnex.us/books/{ASIN}?region=` | Free community aggregator, GPL-3.0, "remains online and maintained" but its README says to start new projects on **AudiobookDB** (https://github.com/laxamentumtech/audnexus README line 19). | API spec: https://api.audnex.us/docs lists `GET /books/{ASIN}` with `region` (au, ca, de, es, fr, in, it, jp, us, uk), `update`, `seedAuthors`, plus `/authors`, `/books/{ASIN}/chapters`. Public **unauthenticated DELETE** endpoints are listed, so never call them. No terms of use are published for the public instance (README and https://audnex.us/ give only the GPL-3.0 licence). Rate limit (observed 2026-11-02, response headers): `x-ratelimit-limit: 300`, `x-ratelimit-reset: 60`, i.e. 300 requests per 60 s per client; the self-host default is 100 per minute (README "Rate Limiting"), so the public instance is configured differently, and nothing promises it will stay. A 429 `RATE_LIMIT_EXCEEDED` is the documented overrun error (README line ~126, ~205). Response fields: asin, title, subtitle, authors, **narrators**, image, formatType (abridged/unabridged), language, releaseDate, runtimeLengthMin, seriesPrimary, isbn (optional), region (`src/config/types.ts:111-140`). |
| AudiobookDB https://audiobookdb.org (docs https://audiobookdb.org/docs, terms https://audiobookdb.org/terms) | Declared successor to Audnexus; keyed API. | **ASIN lookup:** `GET /api/audiobooks/external/audible/{ASIN}` returns the book plus `matchedReleaseId`; "Audible ASINs are release-scoped", so read ASINs from `GET /api/releases/{id}` `external[]`; unknown ASIN returns 404. **Auth:** `X-API-Key` header; key needs a verified email; a meaningful `User-Agent` is mandatory for registered tiers (400 otherwise). **Tiers:** Anonymous 5 req/s, 500 cost units/day; **Registered (free account) 10 req/s, 10,000 units/day**; Editor 50,000; Ecosystem Partner 200,000 (free on approval under written terms); Commercial custom licence. Detail call = 1 unit, search = 3. **Terms that matter:** default licence is "personal, non-commercial use only" (§6.1); programmatic access "require a separate licensing agreement" (§6.2); mandatory attribution and link (§6.3); "You may not cache or store AudiobookDB data longer than is reasonably necessary to serve your application's users" (§6.4), which conflicts with Requestarr's local catalog cache; cover art and descriptions are excluded from the API licence (§5.1); cached data must be deleted on key revocation (§7.4); no regional behaviour is documented. A self-hosted app used by one household is arguably personal use, but the §6.2 wording on programmatic access is ambiguous: **could not be determined** without asking the operator. Not a safe default dependency for v1. |
| Amazon Product Advertising API | Exists, requires an affiliate account. | **Not researched**; out of scope for a self-hosted, no-key v1. |

Recommendation: Audnexus first, Audible catalog as fallback, behind a narrow resolver interface so AudiobookDB can replace either. Cache aggressively; resolution happens only when a user submits a request, never on UI render (AGENTS.md rule 10). Add a descriptive `User-Agent` and a bounded response size; Requestarr already requires this for outbound calls.

### The ASIN versus region problem (observed)

- The same ASIN is **not valid in every Audible marketplace**. Observed on 2026-11-02: `GET https://api.audnex.us/books/B002V0QK4C` returns 200 (US), while `?region=uk` returns HTTP 404 with `{"error":{"code":"REGION_UNAVAILABLE","message":"Item not available in region 'uk' for ASIN: B002V0QK4C"...}}`. The Audnexus README lists `REGION_UNAVAILABLE` as a first-class error.
- An ASIN identifies a **release**, not a work: AudiobookDB states "Audible ASINs are release-scoped" and separates books from releases (https://audiobookdb.org/docs; Audnexus README line 19 describes the successor's "proper book/release separation"). A given recording can therefore exist under several ASINs, and abridged versus unabridged is a property of the release (Audnexus returns `formatType`; ABS stores `abridged`: `Book.js:568-590`).
- ASIN syntax: Audnexus accepts `B` + 9 alphanumerics or 9 digits + digit/`X` (`asin10Regex`, `src/config/types.ts:9-19`); ABS accepts any `[A-Z0-9]{10}` (`isValidASIN`, `server/utils/index.js:260-263`). Requestarr should use the looser ABS rule for validation after upper-casing and trimming, and treat the resolver's 404 as the real check.
- **URL shapes that carry an ASIN.** Neither ABS nor Audnexus parses ASINs out of URLs: ABS takes a bare ASIN typed into the match box (`BookFinder.js`, `Audible.js:137-146`), and Audnexus's only URL parsing is for genre ids (`getGenreAsinFromUrl`, `src/helpers/utils/shared.ts:53`). BookOrbit only *builds* links: `https://www.audible.com/pd/<id>` (`client/src/features/book/lib/provider-links.ts:87`) and `https://www.<amazon-domain>/dp/<id>` (`provider-links.ts:63`, `server/src/modules/file-write/file-write.constants.ts:57`). So URL parsing must be Requestarr's own. Observed 2026-11-02: `https://www.audible.com/pd/B002V0QK4C` answers 301 to `https://www.audible.com/pd/Wizards-First-Rule-Audiobook/B002V0QK4C` (200), confirming `audible.<tld>/pd/[<slug>/]<ASIN>` carries the ASIN as the last path segment; `audible.co.uk` answered 503 to the same request (not determinable). `https://www.amazon.com/dp/B002V0QK4C` and `/gp/product/B002V0QK4C` both returned 404 for this audiobook ASIN, so Amazon store URLs are **not reliable** for audiobook ASINs; whether `/dp/` and `/gp/product/` work for other audiobook ASINs, and whether Amazon's bot handling caused those 404s, could not be determined. Recommendation: accept Audible `/pd/` URLs and bare ASINs in v1 and treat Amazon `/dp/` and `/gp/product/` as best-effort extra patterns (regex `/(?:dp|gp/product|pd/[^/]+)/([A-Z0-9]{10})`), with the TLD as a region hint only.

## 4. Fit with Requestarr

### Mapping onto the existing architecture

| Concern | Existing | Audiobook change |
| --- | --- | --- |
| Media type | `internal/domain/media.go`: movie, tv, album, song, game | Add `audiobook`. Artwork kind: audiobook covers are **square** (Audible `image_sizes`, ABS covers), so `ArtworkSquare`. A decision record (D0xx) is needed since AGENTS.md lists the V1 media set. |
| Resolver | `metadata.Resolver{CanResolve(*url.URL), Resolve}` for provider URLs | Add a resolver for Audible/Amazon URLs whose canonical ID is the ASIN, plus a bare-ASIN input mode (V1 "requester supplies an ASIN"). Bare input needs a small additive entry point since `Service.Resolve` begins with `ParseURL` (`internal/providers/metadata/provider.go`). Catalog item: `Provider=audible`, `ProviderID=ASIN`, `Metadata` carries region, narrators, abridged. |
| Source kind | `internal/sources/sources.go` registry: slug, media types, credential fields | Add an `audiobookshelf` kind: URL plus API key (a Stored secret, D024), `MediaTypes=[audiobook]`. |
| Adapter | `inventory.Source.Snapshot` returning `Complete` only after a full successful enumeration | New `internal/inventory/audiobookshelf`: list libraries (book type only), page items, emit `Item{MediaType:audiobook, ExternalIDs:{"asin":...}, Title, AddedAt, ArtworkRef}`. Must pass `inventorytest`. Any failed page returns an error and no items. |
| Matching | Stable IDs first, then reviewable title/year candidates (docs/ARCHITECTURE.md "Matching hierarchy") | Exact ASIN equality is the stable match. Title+author candidates feed the existing suggested-matches review. |
| Safeguards | Failed sync never removes; empty snapshots never count as absence | Unchanged. Honour `isMissing` as absent and ignore ebook-only items. |
| Issues/Library | `Issue` is about a Library item | Audiobook issue kinds (audio quality, wrong narrator/edition, missing chapters) are a follow-up. |

### Identity and matching key

- Canonical identity: **ASIN** (`audible` provider, ID = ASIN). One shared request per ASIN satisfies the AGENTS.md rule at the identifier level.
- Library side: `media.metadata.asin` (ABS). Compare case-insensitively after trimming; the request side is already normalised.
- Because the same work can have several ASINs, **two requests for different ASINs of the same book are different requests** in v1. This is a known limitation; do not guess at dedup (a work-level key would need a book/release model like AudiobookDB's, which Audnexus and the Audible catalog response used here do not expose; **could not be determined** whether Audible's catalog offers a work-level id).
- Recording the region on the catalog item is useful for display, but it must **not** be part of the match key: ABS stores one ASIN regardless of the region the user's library came from, with no region attached on the item (`Book.js` has `asin` only).

### Gaps and risks

1. **ASIN absent from library items.** ABS leaves `asin` empty unless the item was matched or tagged. `filter=missing.asin` counts them. Such items can only match through the title+author candidate path, which needs `authorName` in `Item` (the struct has `Title`, `Year`, `Detail` only). Mitigation: put author/narrator into `Detail` and extend candidate review, or document that unmatched items need an ABS "Match" in ABS itself.
2. **Regional/edition mismatch.** A UK ASIN requested but the library holds the US edition of the same recording: no match, request stays open. Mitigation: accept admin-confirmed candidate matches (already scoped to a catalog/library pair), and show the region in the request.
3. **Abridged vs unabridged.** Available data (`abridged`) exists on both sides but is not part of the ASIN. A request for an unabridged ASIN is satisfied only by an item with that ASIN, so the ASIN already encodes the edition; abridged flags matter only for the title-candidate path (treat a mismatch as lower confidence).
4. **Audible multi-part/box sets** and "Dramatized Adaptation" variants (ABS cleans series sequences for these: `Audible.js:24-38`) mean the ASIN may denote a different grouping than the library item.
5. **Metadata upstream fragility.** Resolution depends on an undocumented Audible endpoint or a community API that points to a successor. A resolver outage must degrade to "title unknown, ASIN accepted" rather than blocking requests (the cache and `ErrBusy` stale-serve already exist in `Service`).
6. **Terms of use.** The Audible Conditions of Use are silent on scripted catalog calls and the Amazon documents they incorporate could not be read (see section 3). Keep volume tiny (one call per new request, cached) and say in docs that the upstreams are unofficial. AudiobookDB, by contrast, has explicit terms (key, attribution, no long-term caching, personal use) that make it unsuitable as a default.
7. **Scope drift.** ABS can also *download* podcasts and run RSS feeds; BookOrbit can download books. Requestarr must only read item metadata. Add contract tests that no write method is ever issued.
8. **Credential handling.** The ABS key is a bearer JWT; store as a Stored secret (D024), never log it, header-only (never `?token=`).
9. **ABS performance on large libraries.** `limit=0` returns the whole library in one response; for tens of thousands of items use paging (`limit`/`page`) with `minified=1` and a response-size cap, as the other adapters do.

### Suggested minimal v1 shape

1. Decision record: add `audiobook` media type (square artwork), ASIN identity (a release-scoped id, so two ASINs of one recording are two requests), ABS as the only audiobook Source kind.
2. Request entry: paste an Audible `/pd/` URL **or** a bare ASIN (Amazon `/dp/` and `/gp/product/` as best-effort patterns). Resolve via Audnexus (US by default; use the URL's TLD as a region hint), fall back to Audible's catalog; on failure accept the ASIN with an "unresolved" placeholder title. Do not make AudiobookDB a default dependency (key, attribution, personal-use and no-long-term-caching terms).
3. `audiobookshelf` Source kind and adapter: API key as Stored secret (require ABS >= 2.26.0), issued for a dedicated plain `user` account (not admin; `update`/`delete`/`upload` are off by default), `/api/libraries` then paged `/api/libraries/:id/items?minified=1`, libraries with `mediaType == "book"` only, items with `media.numTracks > 0` only, drop `isMissing`.
4. Matching: exact ASIN only in v1, plus the existing suggested-matches review for title+author candidates.
5. Out of v1: BookOrbit (no machine token; ASIN only via per-book detail calls), region-aware aliasing between ASINs, ISBN matching (ABS `isbn` is in the list payload and BookOrbit `isbn13` is a cheap v1.1 alias), Socket.IO live updates, audiobook Issue kinds.
6. Tests: recorded ABS fixtures (not the stale OpenAPI spec) for pages, missing ASIN, `isMissing`, ebook-only (`numTracks == 0`, `ebookFormat` set), expired/revoked key, truncated page (no removals), and a region-404 from Audnexus; URL-parser table tests for the shapes in section 3.

## Open questions

Answered in the second pass and removed: BookOrbit identity (only candidate found), ABS field names and library `mediaType`, read-only user type, OpenAPI status, ABS license, BookOrbit row DTO / filter / token / events, AudiobookDB terms, Audnexus rate limit, URL shapes.

1. Does the requester mean `bookorbit/bookorbit`? Sources cannot answer this; ask them. If BookOrbit is wanted, a maintainer request for an API token and ASIN in `BookCard` (https://github.com/bookorbit/bookorbit/issues) is the unblock.
2. Do the Amazon.com Conditions of Use or Audible License restrict scripted calls to the Audible catalog endpoint? Both documents were unreadable (HTTP 503 from amazon.com), so this could not be determined; a legal read is needed before shipping a default dependency on it.
3. Is a self-hosted Requestarr "personal, non-commercial use" under AudiobookDB's terms (section 6.1 versus 6.2 on programmatic access)? Only the operator can answer; it is also incompatible with Requestarr's catalog cache under section 6.4 unless refreshed or licensed.
4. Do Amazon `/dp/` and `/gp/product/` URLs resolve for audiobook ASINs generally (two probes returned 404 for one ASIN, and audible.co.uk returned 503)? Needs more samples from a normal browser session.
5. Should a work-level key ever merge requests across ASINs, given the shared-request rule? Needs a product decision; no source offers a work id except AudiobookDB's book/release model.
6. Where does the region come from: URL TLD, a global default, or a per-request pick?
7. How often do ABS items carry the ASIN of a series or box set versus a single book, and how should those match?
8. Should the ABS adapter use `updatedAt` to skip unchanged items between syncs, or always do a full enumeration like the other Sources (current rule favours full enumeration)?
9. Can `download: true` be disabled on the dedicated ABS account without affecting enumeration or cover fetching (covers use `GET /api/items/:id/cover`)? Not tested against a running server.

## Source index

- Audiobookshelf: https://github.com/advplyr/audiobookshelf (files cited above; OpenAPI in `docs/`).
- BookOrbit: https://github.com/bookorbit/bookorbit, https://bookorbit.app.
- Audnexus: https://github.com/laxamentumtech/audnexus, https://api.audnex.us/docs, https://audnex.us/.
- AudiobookDB: https://audiobookdb.org, https://audiobookdb.org/docs, https://audiobookdb.org/terms.
- Audible Conditions of Use: https://www.audible.com/legal/conditions-of-use.
- Audible catalog (undocumented): `https://api.audible.<tld>/1.0/catalog/products` (observed, not documented).
