# Architecture / Product Decision Log

These are accepted decisions unless explicitly superseded by a dated entry.

## D001 — No acquisition layer
Requestarr only manages requests, availability, and notifications. No *arr/downloader integration in V1.

## D002 — Link-first V1
Users paste metadata provider links instead of searching an internal/global catalog. Native discovery can be added later without changing the request modal/domain path.

Superseded in part by D032: name search is now allowed as a front door to the resolver. The rest of D002 (no internal/global catalog, no discovery) stands.

## D003 — Shared single request
One canonical request exists per item. The requester's identity is attribution, not privacy/ownership. Requests are visible to every authenticated user.

## D004 — Notify Me subscriptions
The requester is subscribed automatically. Other users may subscribe to request state changes without creating a duplicate request or vote.

## D005 — OIDC required
OIDC is mandatory for normal V1 operation. Keycloak is the primary tested provider. First-run bootstrap uses a temporary setup mechanism only.

## D006 — Apprise integration
In-app notifications are native; external notifications use Apprise API instead of implementing many destination-specific adapters.

External Apprise delivery uses one operator-configured global route (environment URL + configuration key), enqueued once per event; native inbox delivery remains subscriber-scoped. Migration 000010 is retained unchanged and its user-level Apprise preferences are deprecated. Migration 000011 introduces the dedicated global outbox.

## D007 — Library authority
Plex/Jellyfin determine movie/TV availability, Plex/Jellyfin/Navidrome determine music availability, and RomM determines game availability. A source is authoritative only for configured media types.

## D008 — Local state + background sync
UI state is primarily served from local PostgreSQL. External slowness/outages should not block normal pages. Source failures retain last-known inventory.

## D009 — Game platforms are dynamic OR criteria
IGDB defines which platforms to display. UI shows Any first and then provider platforms as multi-select checkboxes. Any selected platform may satisfy the request.

## D010 — Native artwork proportions
Poster media remains ~2:3; album/song artwork remains square. UI normalizes layout containers, not the art itself.

## D011 — Shared Requests navigation
Primary navigation says `Requests`, not `My Requests`. My Requests is a filter/button on the shared page.

## D012 — Setup wizard is V1
First run guides General -> Authentication -> Metadata Providers -> Libraries -> Notifications -> Request Policy -> Review -> Finish, with external connection validation and resumability.

## Migration 000007 — Correct in-place before release
Migration 000001 already creates and seeds the singleton `setup_state` row. Migration 000007 extends that table with admin-bootstrap fields and must not create a second table or seed row. Editing 000007 in place is safe only because it is newly authored, unmerged/unreleased, and has not been applied in production; once shipped or applied, migrations are append-only and corrections require a new migration.

## D013 — License is AGPL-3.0
Requestarr is a network service; AGPL-3.0 keeps hosted forks open. The Go module path is `github.com/evilperson1337/requestarr`, matching the real remote. The default branch is `main`.

## D014 — Request status `available` is projected from Availability
When a Source sync makes a catalog item available, open Requests for it move to `available` in the same transaction, with a version bump and an audit event, and the `RequestAvailable` event is emitted from that path. The unused `internal/reconcile` package was removed; availability matching lives in `internal/store/postgres/inventory.go` until the adapter interface (ticket 11) is designed.

## D015 — Availability is "present in at least one Source"
An item becomes missing only after every covering Source completes a successful sync without it. Failed syncs never produce removals. Absence is tracked per (Source, item) in `source_absence_state`: a Source drops its own evidence after two complete syncs a day apart without the item, and the item turns missing only when no enabled Source covering its media type still has evidence. Every adapter implements `inventory.Source` and must pass `internal/inventory/inventorytest`.

## D016 — Cancellation of shared Requests
A Requester cancelling leaves the Request open if other Subscribers remain; it becomes `cancelled` only when the last Subscriber leaves or an admin cancels it. Admins can set `viewer` as well as `requester`/`admin`, and the last admin cannot be demoted. Leaving is idempotent by state and takes no operation key; a leave that would cancel the Request must be confirmed explicitly (`confirmCancel=true`), so the last-subscriber warning is decided by the server, not a stale page. Admin cancel uses the existing transition `expectedVersion`/`operationId`.

## D017 — Secrets stay environment-only in V1 (partly superseded by D024)
The setup wizard persists non-secret settings (URLs, library choices) and the names of environment variables holding secrets. Encrypted secrets in PostgreSQL are deferred. The wizard must state clearly what still needs an env edit and restart.

## D018 — V1 job runner is Postgres-backed and minimal
A job-run table with status, timestamps and errors, a no-overlap guard that survives restarts, an admin "sync now" action and a status endpoint. No queue library or worker process; retries use the next scheduled tick. Lease and run times come from the database clock, a job whose lease cannot be renewed is cancelled, and each Source keeps its newest 200 runs.

## D019 — V1 build order
Baseline hygiene, then the `available` flow, then the job runner, then the adapter interface with Plex, RomM, Jellyfin (onto the new interface) and Navidrome in that order, then the Requests page, then wizard/settings/ops status, then release hardening.

## D020 — Music identity is release-group and recording, bridged to library ids by aliases
An album Request's canonical identity is a MusicBrainz release group and a song's is a recording; release ids are never equated with release groups, and a pasted release link resolves to its group. Library Sources often expose other identifiers (Plex: release and release-track MBIDs), so an alias table (`catalog_item_aliases`, kind `mbid`) maps them to the catalog item. Aliases are looked up from MusicBrainz, rate-limited and best-effort, only for open music Requests, and matched per media type; a failed lookup delays a match but never changes inventory.

## D021 — Game availability is per platform
Availability evidence and absence streaks carry a platform key (`igdb:<id>`, or `''` for non-game media and platforms a Source cannot map to IGDB). A game Request is moved to `available` only when the evidence satisfies its selection (`domain.PlatformSelection.SatisfiedBy`, D009): any selected platform for an explicit selection, any platform at all for Any. Evidence with an unknown platform satisfies Any but never an explicit selection. A game is available as a catalog item whenever any platform holds it; each platform's evidence expires on its own.

## D022 — Development OIDC issuers may use loopback HTTP
`REQUESTARR_OIDC_ISSUER` stays HTTPS-only, except that with `REQUESTARR_ENV=development` an `http` issuer on `localhost`, `127.0.0.1` or `::1` is accepted so the dev compose Keycloak (plain HTTP) can be used. Production and non-loopback hosts still require HTTPS. OIDC remains the only sign-in path (accepted V1 decision 5). `make check-login` verifies the flow.

## D023 — Supply-chain and container publishing policy (proposed)
Releases publish a multi-arch container image to GHCR, an SPDX SBOM per archive and image, build-provenance attestations, and a keyless cosign signature on the image; `latest` tracks stable tags only. Details are in `docs/RELEASES.md`. Proposed during ticket 28; it becomes accepted when the maintainer approves it. the workflow implementation is still outstanding.

## D024 — Post-bootstrap integration secrets may be stored encrypted
Partly supersedes D017. An admin may enter the TMDB token, IGDB client secret, Jellyfin/Plex/RomM credentials, the Navidrome password and the Apprise config key in Setup. They are stored in PostgreSQL encrypted with AES-256-GCM under an operator-supplied master key (`REQUESTARR_SECRETS_KEY`, 32 bytes, validated at startup); each row records its key id. Without the master key the UI refuses secret values and the environment path keeps working.

- **Stays environment-only:** the OIDC client secret and the session HMAC secret (sign-in must work before Setup is reachable) and the master key itself. None is auto-generated into the database.
- **Precedence:** an environment value wins over a stored one. Each secret reports its source: `environment`, `stored`, or `stored (shadowed by environment)`. Shadowed values warn on Review and are never deleted automatically.
- **Write-only:** a stored secret is never returned, logged, audited or echoed in errors; only a presence flag and its source are exposed.
- **Activation:** stored secrets apply on restart, like the rest of the inactive draft; Review lists them as "saved, pending restart". Hot reload is a separate later decision. Startup resolves each secret (environment, then database) before `Validate`, so a stored secret satisfies the URL-plus-secret pairing rules. Library and Apprise URLs stay environment-only.
- **Fail closed:** a missing or wrong master key does not stop boot; the affected integration reports "stored secret unreadable" and is not treated as unconfigured or guessed.
- **Checks:** connection checks use the value the next restart would use and say which source it came from.
- **Change after Finish:** replacing or clearing a secret stays available after Finish through a dedicated admin endpoint (CSRF and Origin protected) with value-free audit events in the same transaction. The rest of the draft stays read-only.
- **Navidrome:** the username becomes an ordinary draft field; only the password is a secret.
- **Rotation and backup:** key rotation tooling is deferred; a changed key means re-entering secrets. Docs must say the master key is backed up separately from the database.

Implementation notes (ticket 02-07):

- **Master key format:** 64 hex characters, or base64 decoding to 32 bytes. Ciphertext is bound to the secret name as AES-GCM additional data, so a row moved to another name does not open.
- **Audit:** value-free set/clear events go to a separate `secret_audit_events` table, written in the same transaction as the change.
- **Delete without a key:** `DELETE /admin/secrets/{name}` works without a master key so an unreadable secret can be discarded.
- **Unreadable at boot:** an unreadable stored secret whose Source or Apprise URL comes from the environment blanks that URL (for Navidrome also the username) so the integration stays off as a unit instead of failing the pairing rule.
- **Navidrome username:** seeded from `REQUESTARR_NAVIDROME_USERNAME` when the draft's username is empty.

- **Draft field rename:** the Navidrome draft field `usernameEnv` became `username`. Old drafts still load. A stale client that sends `usernameEnv` gets a 400 until it reloads.
- **Value limits:** a secret value is 1-4096 characters, with no leading or trailing whitespace and no control characters.

## D025 — Saved IGDB client IDs follow restart-bound credential activation

The environment-only client-ID prerequisite conflicted with D024's UI-managed
IGDB secret: entering both credentials in Setup still failed locally, and a
restart did not activate the saved ID. Replace that prerequisite narrowly:

- The non-secret IGDB client ID already persisted in the setup draft is the
  startup fallback when `REQUESTARR_IGDB_CLIENT_ID` is absent. Saving does not
  hot-reload the provider, and does not require wizard completion.
- Connection checks use the saved ID plus the resolved secret. An explicit
  environment ID takes precedence and must match the draft before checking.
- ID writes keep the existing admin-only, revisioned, auditable draft path.
  Startup only reads the existing draft; no schema migration or state reset is
  needed. Other non-secret settings, including library/Apprise URLs and OIDC,
  retain their existing activation rules.

## D026 — Sync health has one read model

A Source's health was reported two ways: `library_sources.last_sync_*` through a
Jellyfin-only endpoint, and `job_runs` through the operational status view.
Replace the Jellyfin-only path:

- Run outcomes (latest run, error, last success) come from `job_runs` (D018).
  The last successful snapshot time comes from `library_sources`. Both are
  returned per Source by `GET /admin/status`; a failed run never moves
  `lastSnapshotAt`.
- `GET /admin/sources/jellyfin/status` is removed. The Settings page renders
  every Source from `GET /admin/status` and can start a sync for each.
- A Source ID is defined once, beside its adapter (`jellyfin.SourceID`, like the
  other three).
- `library_sources` is written by one upsert: a successful snapshot refreshes
  its identity row, a failure only ensures the row exists. The unused
  `last_sync_started_at` and `last_sync_error` columns are still written and are
  dropped by a later migration (migrations are append-only).

Extended by the architecture-deepening work:

- **Source kinds** (Jellyfin, Plex, RomM, Navidrome) are one registry in
  `internal/sources`: slug, stable ID, media types and ordered credential
  fields. Setup, configuration, startup wiring and the wizard read it;
  `GET /admin/setup/source-kinds` serves it to the web app.
- **IGDB pairing:** the saved client ID and the secret are entered separately
  (D025), so no pairing rule stops startup. IGDB is reported as not
  configured until both exist.
- **Startup** resolves in one order (`resolveBoot`: saved IGDB ID, secrets,
  validation) and `app.Build` derives the runtime from the result.

## D027 — A Request can cover part of an item

Accepted (this supersedes the earlier "a series Request always covers the whole series" wording).

A Request still belongs to one catalog item and an item still has one shared Request. What changes is that a Request now carries a **selection**: the part of the item it covers. An empty selection is the whole item, so every earlier Request keeps its meaning.

- An album selects tracks (snapshotted id, position and title); a series selects whole seasons and single episodes of seasons not selected whole. Games, movies and songs have no selection.
- Selections merge by union into the existing open Request and never narrow it. Whoever widens a Request is subscribed to it as `manual` and the change is recorded as a `request.selection_changed` event. Requesting the whole item absorbs any subset.
- Availability is unchanged for now: it is still projected for the whole item. Per-track and per-episode availability needs episode/track evidence from each Source adapter and is a later step. Until then a partial Request becomes available when its item does.
- Track lists come from MusicBrainz and season/episode lists from TMDB, fetched when the dialog opens. If either lookup fails, the dialog falls back to requesting the whole item.

## D028 — Library connections are managed in Settings and apply at once

Accepted. This supersedes the "library and Apprise URLs stay environment-only" part of D024 for **library Sources only** (Apprise, OIDC and the other URLs are unchanged).

- A library Source's address (and Navidrome's username) can be saved in Settings (`library_connections`). Its credential stays a D024 stored secret.
- **The environment wins.** An address from `REQUESTARR_*_URL` is used as is and the saved one is shadowed; Settings shows it as set by the environment and will not change it. This keeps every existing deployment working untouched.
- **Changes apply immediately, with no restart.** Saving an address, saving or clearing its credential, or removing the connection rebuilds that one Source: its schedule is stopped and, when the connection is complete, started again. A Source with an address but no credential stays off and Settings says what it is missing. A failed rebuild leaves the other Sources running.
- **Boot never fails on a half-set stored connection.** A stored credential with no address (or the reverse) leaves that Source off with a warning, as the unreadable-secret case already does. An environment credential with no address is still a configuration error.
- **Test connection** is the setup wizard's read-only probe run against the Source as currently configured. It never returns a credential.
- Removing a connection stops syncing; inventory already observed is kept and is replaced by the next successful sync of that Source (D015 still forbids removals from a failed one).

## D029 — Home shows what libraries recently gained, with the library's own covers

Accepted.

- Home's "Recently added to your libraries" lists the newest movies, series and albums from each enabled library's latest complete snapshot (so a removed item never shows; songs are left out; the same title in two libraries is one entry). Order is the library's own added date, else first sighting.
- Covers come from the library that owns the item, not from TMDB or MusicBrainz lookups. An adapter that can supply them implements `inventory.ArtworkSource`; it keeps only a validated reference to one item's cover (Plex: `/library/metadata/<id>/thumb[/<ts>]`) and fetches it through the library's own resizer. The credential stays inside the adapter and the browser only ever calls `GET /library/items/{id}/artwork`, which returns an image or a generic error. Libraries without covers (today: Jellyfin, RomM, Navidrome) show a plain tile.
- Every signed-in role can read both endpoints; nothing in them is request- or user-specific.

Amended (same day): Home shows two shelves, movies and shows (posters) and music (squares), so every cover in a row has the same proportions; `GET /library/recent` takes a `types` filter for this. A title resolved after its library synced is matched against the inventory already stored (`matchExistingInventory`), so its availability is known at once; the preview always names the libraries that hold it (a link only when safe); and a Request for something a library already has is refused (`409 already_available`) unless a Request already exists.

Amended again: games are a third shelf (3:4 frame, portrait covers cropped to fill, caption "Game · <platform> · RomM"). The RomM adapter supplies the added date (`created_at`), the platform's display name (`inventory_items.detail`) and fetches the ROM's own cover (`/assets/romm/resources/roms/<platform>/<id>/cover/big|small.<ext>`) through its token, never IGDB.

Amended again: each adapter now records where its media server shows an item (`inventory_items.external_url`): Plex Web (`/web/index.html#!/server/<machineId>/details?key=<item>`, the server id read once from `/identity`), Jellyfin (`/web/index.html#!/details?id=`), RomM (`/rom/<id>`) and Navidrome albums (`/app/#/album/<id>/show`). The request dialog's "Plex: Available" tag opens it there in a new tab. The link uses the address Requestarr was configured with, so it only works where the browser can reach that address; it carries no credential and is dropped if it ever would (`safeLibraryLink`).

## D030 — Library items are browsable, and an Issue is a report about one

Accepted.

People can browse what each Source holds (the Library page) and report a problem with an item. An **Issue** is separate from a Request: it is about something a Source already has, it attaches to the **Library item** (not the Catalog item), and Requestarr only records it and notifies people. An admin fixes the item outside Requestarr; nothing in an Issue can start a download, replacement or *arr call, so the acquisition boundary in AGENTS.md and V1_SCOPE is unchanged.

- Every signed-in role can report (Viewers included); only admins resolve, close or reopen.
- One shared open Issue per (Library item, kind, part), like one shared Request per item; no vote counts.
- Kinds are fixed in code per media type (video, audio and subtitles for video; audio and tracks for music; boot, region, version and files for games).
- An item that leaves its library keeps its Issue open and labelled; nothing resolves automatically.

The spec and tickets are in `.scratch/library-and-issues/`.

Addendum to D020: when an album is resolved, every release of its release group (already in the MusicBrainz response) is recorded as an `mbid` alias, and the item is matched against the inventory already stored (`matchExistingInventory`). A library that holds the album as any edition is therefore found at once, without waiting for a Request to exist (alias lookups for open Requests during sync remain the fallback for songs).

## D031 — Audiobooks are requested by ASIN and detected through Audiobookshelf

Accepted. Research: `docs/research/AUDIOBOOK_INTEGRATION.md`.

- `audiobook` is a new media type (square artwork). Its identity is the **ASIN** (`Provider=audible`, `ProviderID=ASIN`). One shared Request per ASIN; two ASINs for the same book are two Requests in v1, with no title-based merging.
- A requester supplies an Audible product URL or a bare ASIN. The marketplace region is the admin's global default (US out of the box); a pasted Audible URL's TLD overrides it. There is no per-request picker, and the region is display data, never part of the match key.
- Metadata (title, author, narrator, cover, abridged) is resolved through a narrow resolver: Audnexus first, Audible's catalog as fallback, so AudiobookDB can replace either. If resolution fails, the Request is accepted with an unresolved placeholder title and resolution is retried later; an upstream outage never blocks a Request. Resolution happens only when a Request is created, and results are cached.
- **Audiobookshelf** is the only audiobook Source kind in v1 (>= 2.26.0, API key held as a Stored secret, D024, sent in the Authorization header only). The adapter reads book libraries and treats an item as audio only when `numTracks > 0`; `isMissing` counts as absent; a failed or truncated enumeration yields no snapshot (D015). It never calls a write endpoint.
- Matching is exact ASIN equality plus the existing reviewable title/author candidates. ISBN aliasing is deferred to v1.1.
- BookOrbit is not supported: no machine API token, no ASIN in list rows, and its own request and download features overlap the acquisition boundary in AGENTS.md.

Amended (when accepted):

- The Source's identifier kind is `audible`, the catalog provider name, not `asin`; that is the key the matcher uses.
- The default marketplace is the environment setting `REQUESTARR_AUDIBLE_REGION`, not an admin UI field.
- Suggested matches compare the normalised title only; the Audiobookshelf adapter leaves `Year` empty so Audible's recording year cannot hide a candidate for an item with no ASIN.
- A placeholder for an unresolved ASIN is marked `metadata.unresolved`, is never cached, and never overwrites stored details. Nothing retries it in the background yet.
- Follow-ups, not decided: ASINs of series or box-set parents against single books; skipping unchanged items by `updatedAt`; Amazon product URLs (not confirmed to carry audiobook ASINs); author-aware matching; audiobook Issue kinds; a retry job for placeholders; ISBN aliasing (v1.1); a nicer message than HTTP 400 for an ASIN no marketplace knows.

## D032 — Name search is a front door to the resolver

Accepted 2026-11-04. Research: `docs/research/SEARCH_INTEGRATION.md`. Spec: `.scratch/name-search/spec.md`.

D002 stays accepted. This decision supersedes only its "no search" part: people may find an item by name instead of pasting a link.

- A **Search result** is a pointer, not a Catalog item. Its `Ref` is a canonical URL (or bare ASIN) that the same provider's resolver already accepts. Choosing a result calls the existing `POST /resolve`, so the preview, `catalogId` upsert, `POST /requests`, subscriptions and part selections (D027) are unchanged. Search creates no new request path and no sparse catalog rows.
- Search never writes. It annotates results from local PostgreSQL only ("requested", "in library"); it never creates a Request, a Catalog item or an Issue.
- It is a post-release V1.x feature, phased by provider risk: Movies & TV (TMDB), games (IGDB), albums and songs (MusicBrainz), audiobooks (Audible catalog).
- Discovery, trending and recommendations stay out of scope. They need their own decision.
- Search is a fourth interactive caller of providers beside URL resolution, connection tests and background jobs. Page rendering still reads local state only.
- Assumption (recorded by this decision, not verified with the providers): a self-hosted, free, AGPL Requestarr counts as non-commercial use of TMDB, MusicBrainz and IGDB. Image CDNs (TMDB, Cover Art Archive, IGDB) are hotlinked in the browser, as previews already do. If a provider says otherwise, this decision is revisited for that provider.

Open items, not decided:

- The Twitch Developer Service Agreement (IGDB) has not been read; whether it limits caching search responses is unknown.
- The Cover Art Archive terms have not been read; they matter once search shows many album covers at once.
- TMDB attribution (logo and non-endorsement notice) must be confirmed or added before TMDB search ships.
- The other open questions in the research note (blended "All" type, per-user rate limits, per-provider disable) are settled in the spec's tickets, not here.

Addendum (Songs, ticket 09): a Songs search row is one MusicBrainz recording, with the recording URL as its ref. The search status badge ("In library") for album and song results matches a library's items by the key's own id, by a Plex `mbid` equal to it, and through the catalog item's aliases (D020). An item nobody has opened has no catalog row and so no aliases; a copy held in Plex under a release or release-track id is then reported as unknown (no local state, no badge), never as absent. Aliases arrive when an album is previewed (its releases) or when a song has an open Request (alias job). Reading release or track ids out of the search response itself would close that gap and is a possible follow-up.
