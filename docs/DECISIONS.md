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

## D023 — Supply-chain and container publishing policy
Releases publish a multi-arch container image to GHCR, an SPDX SBOM per archive and image, build-provenance attestations, and a keyless cosign signature on the image; `latest` tracks stable tags only. Details are in `docs/RELEASES.md`. Proposed during ticket 28 and accepted with D034. The workflow is implemented in `release.yml` but unconfirmed until a tag is pushed.

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

Amended: the Library dialog shows what the library itself says about an item, not a lookup elsewhere (the D029 rule for covers, applied to facts). Each adapter that has them records a synopsis, an audience rating on a 0-10 scale, a certification, genres and a runtime in `inventory_items.details` on every sync (every adapter but only what its library knows: Plex, Jellyfin and RomM give a synopsis, rating, genres and runtime; Navidrome albums give genres and runtime; Audiobookshelf gives a description, genres and runtime, with HTML descriptions reduced to text). Plex and Navidrome albums also record their artist as the item's `detail`. `GET /library/items/{id}` returns them as `details`; the list does not, to keep pages small. Values are bounded when stored (`LibraryDetails.Clean`). Items show them after their library's next sync. The dialog's "Open in <source>" is a button when the item has a safe link.

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

## D033 — Library Source URLs may use http on a private host

Accepted. Amends the "HTTPS outside development" rule for library Source URLs in `docs/INTEGRATIONS.md`; it does not touch `REQUESTARR_PUBLIC_URL`, the OIDC issuer or the Apprise URL, which stay https-only outside development.

A Source URL (Plex, Jellyfin, Navidrome, RomM, Audiobookshelf), from the environment or saved in Setup, may use `http://` when its host is a **private host**: a loopback, RFC 1918, link-local or CGNAT (100.64.0.0/10) IP literal, a single-label name such as `plex`, or a name ending `.local`, `.lan`, `.internal` or `.home.arpa`. An operator can extend the list with `REQUESTARR_HTTP_ALLOWED_NETWORKS`, a comma-separated list of CIDRs and hostname suffixes; it only ever adds. Any other host still needs `https://`.

Why: a self-hosted library is normally reached over a LAN or a Docker network, so the https-only rule made the Unraid template unusable. The check is syntactic, because a Docker name resolves only at run time, and a library token then travels in cleartext on that network. The documentation says so.

## D034 — Unraid deployment, and a scrubbed public export

Accepted. Plan: `.scratch/unraid-deployment/`.

- **Shape:** a Community Applications-style template XML is the target; a compose file (Requestarr and PostgreSQL only) is for first testing. Both live under `deploy/unraid/` with `docs/UNRAID.md`, and the template installs through Unraid's Template Repositories URL. A Community Applications listing is a later follow-up, after the first release and a test on a real Unraid server.
- **Not bundled:** PostgreSQL (the template expects an external one), the identity provider and TLS. A TLS reverse proxy is a documented prerequisite; the https rule for the public URL and issuer stays (see D033 for libraries). The docs describe OIDC generically with a Keycloak worked example.
- **Secrets:** `REQUESTARR_SESSION_HMAC_SECRET` and `REQUESTARR_SECRETS_KEY` are required, masked template fields with no default, and the field text gives the generation command. The template offers Unraid's Network Type, since container names resolve only on a user-defined network.
- **Health:** the binary gains a `healthcheck` subcommand (a GET of `/readyz` on the local listener), because the distroless image has no `curl`. The Dockerfile and compose use it.
- **Publishing:** accepts D023. The image goes to `ghcr.io/evilperson1337/requestarr`. The first tag is a pre-release (`v0.1.0-rc.1`), tested on a real Unraid server with the compose file pinned to it; stable `v0.1.0` follows, and the template then defaults to `latest`.
- **Public copy:** `git.example.com` stays the source of truth. The GitHub repository is a rewritten export, not a mirror: `scripts/export-public.sh <tag>` runs `git filter-repo` over a fresh clone, changes the module path to `github.com/evilperson1337/requestarr`, replaces every other `example` string and maps all authors to a GitHub noreply identity, then refuses to publish unless no ref, commit message, author, filename or blob contains `example`. The rewrite is deterministic, so hashes are stable between runs. If the gate cannot be satisfied, fall back to one snapshot commit per release. The script is run by hand at first; CI later.

## D035 — People policy: a default role for first sign-in, and admins are told

Accepted. Plan: `.scratch/ux-review/` (ticket 02).

- **Setting:** "default role for first sign-in", `viewer` (the default) or `requester`. It is never `admin`: the API answers 400 `invalid_default_role`, and a stored value other than `requester` is read as `viewer`. It applies only to users created after it is set; an existing user's role never changes. Administrators read and change it in Settings, Users (`GET`/`PUT /admin/settings/people`, admin and superuser only, CSRF on change). The least-privilege floor stays `viewer` unless an administrator opts a deployment into `requester`; no identity is ever auto-admin. (Amended by D041: `admin` and `superuser` come only from an explicit group mapping or from a superuser, never by default.)
- **Notice:** the first sign-in of a new identity sends every administrator an in-app notification, "<name> signed in for the first time as <role>", linking to Settings, Users. It is written in the same transaction that creates the user, keyed `user.first_sign_in:<user id>`, so it goes once per administrator; a repeat sign-in sends nothing and the person who signs in is never told about themselves. It is in-app only (not the global Apprise route). A notification gains an optional `link`, an application path starting with `/`.
- **Settings store:** one small table, `app_settings(key, value, updated_at)`, with a generic `Setting`/`SetSetting` pair on the PostgreSQL store and typed accessors on top (`DefaultFirstSignInRole`/`SetDefaultFirstSignInRole`). Values are plain, non-secret text; secrets stay in D024's encrypted store. Where an environment variable exists for a setting the environment still wins, as in D024; this setting has none. Later policy settings (the auto-approve policy) reuse the table.
- **Viewer message:** the "cannot create Requests" text names the role and tells the person to ask an administrator.

## D037 — Claim the first admin with the token only

Accepted. Plan: `.scratch/ux-review/`. Narrows the claim form described in `docs/OIDC_AUTH.md`; it does not change what the token proves.

- The `setup-token` token stays bound to one issuer and subject, valid 30 minutes, single use. The shell on the server (the ability to run `setup-token`) stays the real gate.
- `POST /setup/claim-admin` takes the token only. Issuer and subject come from the verified session. A request that still sends them must equal the session's, or it is rejected with the same generic 403. CSRF, origin, session checks, the generic failure and the audit events are unchanged.
- While first-admin setup is unclaimed, `GET /auth/me` returns the caller's own verified issuer and subject as `firstAdminClaim` (with `issuerIsConfigured`). It is the narrowest place: it is the caller's own session, it is `no-store`, and it is omitted once an administrator exists, when the status lookup fails, and for anyone unauthenticated. The claim page uses it to show the exact `docker exec ... setup-token --subject '...' --stdout` command. The startup log line and every other route, error and audit text carry no identity.
- `setup-token --issuer` defaults to `REQUESTARR_OIDC_ISSUER`; an explicit flag still wins and is validated as before.

Threat model. An attacker who can sign in but has no shell on the server can:
- become a viewer (unchanged), and read their own issuer and subject (values they already know from their identity provider, and which cannot be used by anyone else);
- submit claims, which fail without a token. The token is 32 random bytes held only as a hash, so guessing it is infeasible; claim traffic should still be rate limited at the reverse proxy.

They cannot:
- obtain a token (only `setup-token` run on the server issues one);
- claim for another person, because the token is matched against the issuer and subject of the session that submits it, and another user's identity is never shown to them;
- see anyone else's identity, or any identity once an administrator exists;
- reuse a spent or expired token.

Showing a user their own identity moves no trust: knowing a subject was never the secret, the token is. Before this decision the operator typed issuer and subject by hand; the server already held them.

## D036 — Approval policy: auto-approve by media type, and a count of what waits

Accepted. Plan: `.scratch/ux-review/` (ticket 03).

- **Setting:** "auto-approve by media type", a set of `movie`, `tv`, `album`, `song`, `game`, `audiobook`; the default is none, so nothing changes until an administrator chooses. Stored in the settings store of D035 (`approval.auto_types`, a comma-separated list read back in a fixed order; a name that is not a media type is ignored on read and rejected with 400 `invalid_media_type` on write). Administrators read and change it in Settings, Users (`GET`/`PUT /admin/settings/approvals`, admin only, CSRF on change).
- **Effect:** a Request created for an auto-approved type is created `approved`. It records a `request.approved` event whose data carries `auto: true` and `reason: policy` (the actor is the requester, who triggered it, not an administrator), and it notifies its subscriber in-app and through the global Apprise route exactly as a manual approval does. The policy is read when a Request is created; it never changes an existing Request, and a person joining an existing Request changes nothing (no second event). If the policy cannot be read the Request waits for a person. `approved` and `requested` are both open, so no availability logic changes, and this is still one shared Request per item, with no voting.
- **Count:** `GET /requests/summary` (admin only) returns `{requested: n}`, the number of Requests with status `requested`. The admin nav shows it on the Requests item, and that item opens `/requests?status=requested`. It is fetched on every page change and after an approval or denial; requesters and viewers neither see nor fetch it.
- **Notification keys:** request notifications were keyed by the event type alone (`request.approved`), and the unique index on `(event_key, user_id, channel)` silently dropped every approval notice after a person's first. The key now carries the request id (`request.approved:<id>`, as the Apprise key already did), so each Request is announced once per subscriber.

## D038 — The draft setup wizard is retired; a Get started checklist replaces it

Accepted. Plan: `.scratch/ux-review/` (ticket 05). Supersedes the draft wizard described by D025 and the "inactive draft" rules around it; it does not touch D024, D028 or D037.

- **Retired:** the seven-step draft wizard (General, Authentication, Providers, Libraries, Notifications, Policy, Review) and everything behind it: the `/setup` page, `GET`/`PUT /admin/setup/draft`, `POST /admin/setup/validate`, `POST /admin/setup/finish`, `GET /admin/setup/source-kinds`, the setup service and draft persistence in `internal/setup` and `internal/store/postgres`, `wizardComplete` in `GET /setup/status`, and their tests and OpenAPI entries. `internal/setup` keeps only the connection-check vocabulary the library Test connection and the checker still use.
- **Why:** the draft saved values and applied nothing except one (the IGDB client ID), asked for settings that Settings already applies live (D028), and made a new admin enter a library address and credentials twice. The review found it was the first thing a new administrator met and the least useful.
- **Replacement:** a checklist at `/get-started`, computed from live state and holding no inputs: a library connected and synced and a metadata provider (TMDB or IGDB; MusicBrainz is always on and does not count) are required; notifications (Apprise) and a reviewed people and approval policy (the default role or the auto-approve types saved at least once) are optional. Each item links to the Settings section that applies it. Dismissing is remembered for the instance as the setting `setup.checklist_dismissed` in the settings store (D035); the nav item shows to administrators until every required item is done or it is dismissed. `GET /admin/checklist` and `POST /admin/checklist/dismiss` are the API.
- **IGDB client ID:** now the setting `igdb.client_id`, a field in Settings, Integrations beside the IGDB secret (`GET`/`PUT /admin/settings/igdb`). It applies at once (D039). `REQUESTARR_IGDB_CLIENT_ID` still wins and makes the field read-only.
- **Upgrade:** the `setup_state` and `setup_draft_events` tables and migration 000021 stay (migrations are append-only); they are unused. While no `igdb.client_id` setting exists, startup reads the client ID from the old draft (`setup_state.draft`) as an idempotent read-time fallback, so an installation that saved one keeps it with no action. Saving a value in Settings, even an empty one, supersedes it. The fallback never writes.
- **Consequence:** `docs/FIRST_RUN.md` now describes the claim and the checklist (it was `SETUP_WIZARD.md` until the wizard was retired), and `docs/UNRAID.md` and `docs/MANUAL_TESTING.md` follow.

## D039 — Provider credentials apply at once

Accepted. Plan: `.scratch/ux-review/` (ticket 08). Extends D024 (stored secrets) and D028 (library connections apply live); it does not change request, availability or reconciliation behaviour.

- **Seam:** `providerSet` in `internal/app` holds an atomic pointer to an immutable `providerSnapshot`: the metadata resolver service, the search service (which types are offered), the Apprise client and the provider status list. Handlers (`GET /search/types`, search, link resolve) and the notification worker read the current snapshot on every call, so a call that started on one snapshot finishes on it and nobody sees a half-updated set. MusicBrainz and Audible hold no credential and keep one instance for the process, so their rate spacing survives a reload.
- **Rebuild:** saving or clearing the TMDB read token, the IGDB client secret or the Apprise key (`PUT`/`DELETE /admin/secrets/{name}`), or saving the IGDB client ID (`PUT /admin/settings/igdb`), calls `Reload`, which re-resolves the configuration exactly as startup does (environment first, then the saved client ID and stored secrets, D024) and swaps in a new snapshot. Rebuilds are serialized by a mutex, so the newest save is always the last one resolved. The operational status provider list is republished on each swap.
- **Environment wins:** unchanged. A stored value shadowed by an environment variable changes nothing in the running snapshot and still reports `stored (shadowed by environment)`.
- **Failure:** the write happens first, as in `SaveConnection`; there is no new partial-write behaviour. If the rebuild then fails (for example the configuration cannot be read), the previous working snapshot stays in use, the stored value stays as saved, the secret is still reported `pendingRestart`, and the response is 500 `settings_not_applied`. The error text is fixed and never carries a value.
- **Notifications:** the worker always runs and asks the sender whether Apprise is configured before claiming an event. With no key (or no `REQUESTARR_APPRISE_URL`) events stay pending in the outbox exactly as when no worker ran; a claimed event is delivered with the snapshot current at send time, and leases make each event complete once, so nothing is lost or duplicated across a swap. Clearing the key between the claim and the send releases the claim without counting an attempt (the event is pending again with its earlier attempt count), so a cleared key never moves an event toward its attempt limit.
- **Still needs a restart:** `REQUESTARR_SECRETS_KEY`, every environment variable (including `REQUESTARR_APPRISE_URL` and the Audible marketplace), and removing an environment variable that shadows a stored value.
- **UI:** these fields say "saved and applied"; the "applies after a restart" wording and the Settings group text are gone for them.

## D040 — A built-in local administrator, and OIDC configured in Settings

Accepted. Supersedes the "OIDC is required, no permanent local-password authentication" rule in `AGENTS.md` and the "OIDC-only normal authentication" line in `docs/V1_SCOPE.md`; replaces the first-admin claim flow of D037 and changes D038 and D034 where they depend on it. Plan: `.scratch/local-admin/`. Amended by D041: the local administrator is a superuser.

Why: a new instance needed an identity provider, a reverse proxy and a CLI-issued token before anyone could sign in, so first run was the hardest part of the product. The narrow exception below removes that without turning Requestarr into a password-based app.

- **One local account.** A single built-in **local administrator** (username `admin`, always the superuser role; amended by D041, it was the admin role) signs in with a password. It is the only account with a local password; everyone else signs in through OIDC. It is permanent: it can't be deleted and can be disabled only while another admin exists, so it stays the recovery path when OIDC is misconfigured.
- **Creation.** On a fresh install (no users) it is created at first startup with a random password printed once in the container log; the first sign-in forces a new password. `REQUESTARR_ADMIN_PASSWORD` may supply the initial password for automation and is still forced to change. On an existing instance it is not created automatically: an admin can create it from Settings, Authentication.
- **Recovery.** `requestarr reset-admin-password` prints a new random password once and forces a change (and creates the account when it is missing, see the S4 note); shell access is the gate. It replaces `setup-token`, and the claim page, `POST /setup/claim-admin`, the `firstAdminClaim` field and the startup claim hint are removed. A person becomes an admin when the local administrator promotes them in Settings, Users.
- **Protection.** Failed sign-ins slow down per account and per client address with exponential delay and never lock the account out; every success and failure is audited; errors never say which of the two was wrong; passwords are 12 to 128 characters with no composition rules; a password change ends the account's other sessions. Sessions use the existing opaque, hashed-at-rest tokens with CSRF protection. Passwords are hashed with the standard library's PBKDF2-SHA256 at the current OWASP iteration count (no new dependency); a stronger scheme can replace it behind the same interface.
- **OIDC in Settings.** Settings, Authentication holds OIDC configuration (issuer, client ID, client secret, provider name, enable switch, redirect URI, a discovery test). The client secret is a stored secret (D024, master key required). Changes apply at once; a failing configuration can't be enabled and never replaces a working one. OIDC variables are no longer required at startup.
- **Environment wins, as a unit.** If any `REQUESTARR_OIDC_*` variable is set the whole OIDC configuration comes from the environment, with no per-field mixing: the page shows it read-only, warns when saved settings exist and are ignored (naming what differs, with "Discard saved settings"), and reports which variable is missing when the set is partial.
- **Sign-in screen.** With OIDC enabled: "Sign in with <provider>" first, and an "Administrator sign-in" link to the local form. Without OIDC: the local form only. The local form is always reachable at its own address.
- **Unchanged.** `https://` is still required for the public URL outside development (D034): the sign-in is a password and the cookies are `Secure`. The reverse proxy requirement stays.
- **Sequencing.** rc.3 ships first with the current claim flow; this lands as rc.4.


### D040 note: sign-in abuse protection and client address (review fix S1)

- **Delay on failures only.** The exponential delay (1 s, doubling, capped at 15 s so the held response stays under the server write timeout) is applied to the response of a FAILED attempt, per account name and per client address. A request is never refused or delayed because its keys carry a delay, so the correct password always goes through straight away. Earlier text above that says failed sign-ins slow down "the next attempts" is superseded.
- **Bounded work.** At most 4 password verifications run at once, 2 per account name and 2 per client address; one of the 4 slots is kept for a client address with no recent failure, so a flood from addresses that already failed cannot take the last one. Requests over a bound wait up to 2 s for a slot, then get 429 without any hashing. At most 256 failed responses are held at once.
- **Bounded memory.** Tracked keys are capped at 10,000 per kind. Eviction drops only expired entries or the lowest-count entries that carry no delay; when nothing qualifies a new key is simply not tracked. Flooding made-up usernames therefore cannot reset the `admin` delay.
- **Client address.** `REQUESTARR_TRUSTED_PROXIES` (comma-separated CIDRs or addresses, empty by default, validated at start without echoing the value). When the TCP peer is inside the set, the client is the rightmost `X-Forwarded-For` entry not inside it; otherwise, or when the header is missing or unparseable, it is the peer. Forwarding headers are never believed from anyone else.
- **Residual risk.** A sustained attack against `admin` slows every failed attempt to that account (up to 15 s each) and can occupy the guess slots of its own addresses, but it never blocks the correct password: the reserved slot and the "failures only" delay keep a correct sign-in from a clean address working. An attacker with very many distinct addresses can still consume CPU up to the verification bound. Without `REQUESTARR_TRUSTED_PROXIES`, everyone behind one proxy shares an address, so the address bound (2 concurrent verifications) is shared too and a flood through the proxy can make other visitors' attempts wait or get 429; set the variable to avoid this.

### D040 note: audit growth (review fix S2)

`auth_audit_events` gets a `count` column (migration 000038). The only rows an outsider can create at will, `signin_failure` and `signin_throttled`, are coalesced: at most one row per account name or client address per minute, and at most 60 rows per type per minute overall, each carrying the number of attempts it stands for (the count is added to the row when its window ends, every minute, and at shutdown). Every `signin_success`, `password_changed`, `password_change_failure` and `admin_created` row is written individually. Rows older than 90 days are deleted at startup and then daily.

### D040 note: disabling the local administrator (review fix S3)

"Another admin exists" is not enough: that admin must be able to sign in. Disabling is refused unless another admin exists AND OIDC is currently enabled with an active provider (the live snapshot, not a probe). The check runs server-side inside the same advisory-locked transaction that guards the last-admin rule, so a concurrent role change or disable cannot slip between check and update. Errors: `last_admin` (nobody else) and `no_working_sign_in` (409; "turn on OIDC and make sure another admin can sign in first"). `GET .../local-administrator` reports `canDisable` as the conjunction and exposes `otherAdminExists` and `oidcWorking` so the page can say which half is missing. Enabling is never restricted. Not covered: turning OIDC off (or breaking it) *after* the local administrator was disabled leaves no sign-in path; `requestarr reset-admin-password` resets a password but does not enable a disabled account, so recovery then needs the database. Guarding that direction is left as a follow-up.

### D040 note: recovery creates a missing account (review fix S4)

An instance upgraded with users but no administrator and no local administrator had no way in: Settings, Authentication needs an admin, and the reset command refused. `requestarr reset-admin-password` now creates the built-in local administrator when it does not exist (one transaction under the admin-role lock; random one-time password printed once on stdout, must-change set, `admin_created` audit row, a one-line note on stderr) and resets it as before when it does. Story 7 stands: nothing is created automatically on upgrade, only by an operator with shell access running the command. A disabled account is still not enabled by it.

### D040 note: the issuer URL is not restricted to public addresses (review fix S5)

The OIDC issuer is set by an administrator, and the main use case is an identity provider on the same LAN (a Keycloak or Authentik container), so there is deliberately **no private-address or loopback block** on it. That leaves the "test" and "save" buttons able to make the server fetch an administrator-chosen URL; an administrator can already do much more, and the limits that apply are: the issuer and every endpoint it advertises must be `https` (plain `http` only for loopback in development), URLs carry no credentials, query or fragment; **redirects are never followed**; the response must be `application/json` and at most 1 MiB; the whole discovery has a 10 second deadline (and the HTTP client its own timeout); and the page shows only a fixed phrase for the failure category, never the underlying network error, address or body, so it is not a port scanner beyond "worked" or "did not". These limits are covered by tests in `internal/auth/oidc` and `internal/authsettings`. Revisit if a non-admin role ever gains access to this setting.

### D040 note: smaller review fixes (S6)

The dummy password hash used for unknown usernames is made when the handler is registered, not on the first unknown-username request. Changing the password verifies the current one against the stored hash inside the transaction that holds the credential row lock and writes the new hash, so two concurrent changes cannot both succeed (the loser gets `current_password_incorrect`). The OIDC settings service no longer holds a lock across the network during Test or Save: writers are serialised by a writer-only mutex, state is guarded by a short read-write lock, and Status and the sign-in path read the atomic snapshot without waiting; a new snapshot is still swapped in whole and only after discovery and persistence succeed.

### D040 implementation clarification — runtime Authentication (ticket03)

The runtime API uses discovery-on-save/enable rather than a reusable “Test passed” token: Test is diagnostic, and every enabled candidate proves discovery again before metadata, encrypted secret and audit commit together. No accepted product decision changes. The live provider and issuer travel together through each five-minute sign-in transaction; restarting the single process requires starting that sign-in again. Public options expose only enabled/name. “Working” describes the currently active discovered snapshot, not a network health probe on UI reads. Disabling and discarding remain possible without provider reachability or a usable master key. The recovery-account creation/disable backend is provided for ticket04, with no ticket04/05 UI implemented here.

Note (lockout guards): two ways of ending up with no way in were closed after review. `requestarr reset-admin-password` now also turns a disabled local administrator back on, since shell access is the gate and the account is the recovery path. And while the local administrator is disabled, turning OIDC off, saving it as disabled or discarding its settings is refused (409 `oidc_is_only_sign_in`, failing closed when the local state cannot be read); an environment-configured OIDC is unaffected because it cannot be turned off from the app. Together with the existing rule (the local administrator can be disabled only while another admin exists and OIDC is working), at least one way to sign in always remains reachable from the app or from the server shell.

Note (database start-up retry): a server start waits for a database that is not ready yet: three connection tries, five seconds apart, with each failed try logged and no pause after the last. Only causes that waiting can fix are retried (nothing listening, a timeout, a name that does not resolve, PostgreSQL starting up or out of connections); a rejected login, a missing database, a TLS mismatch and an unparseable URL fail at once. Stopping the server ends a wait immediately. Only `app.Run` retries: `--migrate-only` and `reset-admin-password` still try once. The migrations run once, after the connection succeeds. This exists because two containers can start in either order on a home server and the app does not restart itself.

## D041 — Four roles, and OIDC groups that decide them

Accepted. Amends D035 and D040. Plan: `.scratch/four-roles/`. Amended by D042 (dotted group claims; Settings, Logs is a 20th superuser-only operation).

Why: one admin role did everything, from approving a request to changing how people sign in and where every credential lives, and a new OIDC person always started as a viewer with no way for the identity provider to say who is who.

- **Four roles, in order of power:** `viewer`, `requester` (shown as the user group), `admin`, `superuser`. One domain module (`internal/domain/role.go`) ranks them and answers what each may do; no other code compares role names, and the web app reads the capabilities `GET /auth/me` returns (`request`, `manageRequests`, `manageSystem`, `manageAccess`). A **superuser** has nothing withheld. An **admin** runs the system day to day. A **requester** browses and requests; a **viewer** browses, uses Notify Me and reports Issues, but cannot request.
- **Superuser-only.** The route table has an access level `superuser` beside public, session and admin, and `x-required-role: superuser` marks 19 operations in `api/openapi.yaml`: Settings, Authentication (state, save, test, enable, discard); the local administrator (status, create, disable); stored secrets (list, set, clear, which includes the notification gateway key); library connections (list, save, remove, test); the IGDB client ID (get, save); and the Get started checklist (get, dismiss). Changing anyone's role to or from `admin` or `superuser` is also superuser-only. `PUT /admin/users/{userID}/role` stays an admin route because an admin may move people between requester and viewer. `TestSuperuserOnlyRoutesAreExactlyThese` is the authority.
- **Admin keeps** request approval, denial and cancel and the awaiting count, requests on behalf of a person, Issues, matches, operational status and activity (read only), the approval policy, the default role for new people, the Users list, and the first-sign-in notices. An admin sees Activity, Matches and Users in Settings, not Integrations, Authentication or Get started.
- **No "Sync now" for an admin, on purpose.** The button lives in the Integrations tab, which is superuser-only, so an admin has no way to start a sync. Operational status stays visible to an admin (read only): they can see that a source is failing and tell a superuser.
- **Guards.** The last-administrator guard is a last-superuser guard: it counts superusers who can sign in (the local administrator counts only while enabled) and answers 409 `last_superuser`. Nobody changes their own role. The built-in account is a superuser whose role is fixed (a database constraint keeps it so). Refusals answer 403 and name the role needed.
- **Group mapping** (Settings, Authentication, OpenID Connect card; stored with the OIDC settings, none of it secret, applied at once). `assignRolesFromGroups` is **off by default**, so an upgrade never promotes anyone because a provider already sends groups named `admin` or `owner`. `groupsClaim` defaults to `groups`; `ownerGroup`, `adminGroup`, `userGroup` and `viewerGroup` default to `owner`, `admin`, `user` and `viewer` (a blank value saves as its default). Groups are read from the **ID token only**, by that claim, as a list of text values or one text value; the userinfo endpoint is not called. Names match ignoring letter case and one leading `/`. At every sign-in, with the switch on and at least one match, the most powerful matching role is applied, raising or lowering the role; with no match an existing person keeps their role and a first-time person gets the default for new people. A role changed by groups ends the person's other sessions and writes the role-change audit with source `group_mapping` and the person as actor (manual changes have source `manual`). A mapping that would leave no superuser who can sign in is skipped and logged at warning level, and the person still signs in with their current role. The built-in account is never changed by groups. A person's group list is never stored or logged. Under environment OIDC configuration the mapping is off and the fields are read-only. Leaving a group does not lower a role by itself; only a different matching group or a manual change does.
- **Scopes** are a saved setting (`scopes`, space separated). `openid` is always requested and first. A new setup defaults to `openid profile email groups`; an existing OIDC configuration is stored as `openid profile email` by migration, so an upgrade changes nothing about the sign-in request; the environment configuration requests `openid profile email`. Test reads the provider's advertised scopes and adds a **warning, not a failure** when `groups` is requested but not listed (a provider that does not advertise its scopes still passes).
- **Upgrade.** A migration widens the role checks to include `superuser` and makes every existing admin, the built-in account included, a superuser, so nobody loses access; requesters and viewers keep their roles. An owner demotes people who should be admins at their own pace. Old audit rows keep reading correctly and gain source `manual`.
- **Requests on behalf.** An admin or superuser may create a request for a chosen person (`onBehalfOf`): the person is the requester, is subscribed with source `requester` and is notified; the request records who made it (`madeOnBehalfBy`). The one-shared-request rule holds (if the item has a request the person is subscribed instead) and the approval policy applies as if they had asked.
- **Amends D035.** "No identity is ever auto-admin" now reads: the default role for new people is still only `viewer` or `requester`, and no sign-in becomes `admin` or `superuser` by default; those roles come only from an explicit group mapping that a superuser switched on, or from a superuser. Administrators who read "admin" in D035 for who reads and changes the default role and receives the first-sign-in notice now mean admins and superusers.
- **Amends D040.** The local administrator is a **superuser**, not an admin, and its role is fixed. "Always the admin role", "can be disabled only while another admin exists" and "a person becomes an admin when the local administrator promotes them" now mean superuser: it can be disabled only while another superuser exists, and only a superuser promotes anyone to admin or superuser.

## D042 — Server logs in Settings, and the diagnostics that need them

Accepted. Amends D041.

Why: an operator running the container on a NAS often has no easy shell or Docker log access, and three problems in a row (an OIDC role that did not change, a RomM connection test that failed, a MusicBrainz search that said "busy") could only be diagnosed from the server's own log, which the web UI did not show.

- **Settings, Logs** (superuser only; `GET /admin/logs`, the 20th `x-required-role: superuser` operation) shows the most recent server log records, newest first, with a level choice, a text filter and an optional follow. The records are a copy of what the process already writes to standard output at `REQUESTARR_LOG_LEVEL` or above, kept in an in-memory ring of 1000 (`internal/logbuffer`); nothing is written to disk or the database and a restart clears them. It is superuser-only because records name sources, catalog items and provider errors, the content of the superuser-only Integrations tab. Because it shows exactly what is logged, the rule that credentials, group names, search text and provider bodies never reach a log is what keeps them out of this page; no separate redaction exists, so that rule must keep holding. `TestNoLogFieldCarriesACredentialOrPersonalData` (`internal/guardrails`) fails on any log field whose name suggests a credential, personal data or search text, unless it is allowlisted with a reason. The one line that must carry a secret, the local administrator's first-run password (D040), is logged with `logbuffer.StdoutOnly`: it still reaches standard output and is never kept for Settings, Logs. The development-only connection diagnostics (`/admin/development-logs`) are unchanged.
- **OIDC sign-in diagnostic.** Every OIDC sign-in logs one line saying what the group mapping decided: off; no groups claim in the ID token (warning, with the names, never values, of the claims the token has); no match (with the number of groups); already that role; role set. Group names are still never logged (D041).
- **Every OIDC sign-in failure is logged** with a fixed step and reason (`oidc.SignInError`), never the code, state, a token or provider text: `code_exchange` with `oauth_invalid_client` (wrong client ID or secret), `oauth_invalid_grant` (redirect address mismatch or a reused code), `http_<status>` or `timeout`; `id_token_verify` (`expired_check_server_clock`, or rejected: check issuer, client ID and signing keys); `claims_decode`; `subject_or_nonce`; and the login-transaction steps (cookie missing, transaction expired, storage). OIDC settings saves, tests, enables, disables and discards log the acting administrator's ID and the fixed outcome; startup logs whether OIDC is active or why it is not working.
- **A failed library sync logs the step it was on.** Adapters report where they are with `inventory.SetStep` (a fixed phrase and a page offset, such as `listing games at offset 400`, never a title or library identifier); the sync job logs the last step with the failure. The stored run error stays the fixed string.
- **A saved library address without its credential no longer stops boot.** Settings, Integrations lets an address be saved before (or after losing) its credential and reports what is missing, but startup validation applied the URL-plus-credential pairing rule to it, so the server refused to start (`REQUESTARR_ROMM_URL and REQUESTARR_ROMM_TOKEN must be configured together` with no such variable set) and the operator was locked out of the screen that fixes it. Validation now leaves out a Source whose address came from a saved connection and which is incomplete; the Source manager already holds it off. An address set by environment variable keeps the pairing rule.
- **Amends D041: dotted group claims.** A groups claim whose name exists in the ID token is read as is; otherwise a dotted name is a path through nested objects, so `realm_access.roles` and `resource_access.<client>.roles` (Keycloak roles) can decide roles. Still the ID token only.
- **Connection checks are logged.** A failed library or provider connection check logs its fixed category (target, stage, code such as `response_size` or `http_status`, HTTP status, duration), never an error value, URL, header or body. The RomM check reads up to 8 MiB of `/api/platforms` and requires a JSON list: RomM 5.4 returns each platform with its firmware list, abbreviation and alternative names, and a large library's answer exceeded the former 64 KiB limit, failing the check while the sync itself worked.
- **MusicBrainz name searches retry a busy answer.** MusicBrainz's search servers answer 503 to a fraction of requests even from a client inside its rate limit (about one recording search in four in a measurement on 2026-11-07). A name search now asks up to three times when the answer is busy; every attempt waits for its own slot in the search line of the shared spacer, so the one-request-per-second budget holds and previews are still served first. Other failures are not retried.

## D043 — A read-only diagnostics token, so an agent can see a running instance

Accepted. Builds on D041 (roles, superuser-only routes) and D042 (Settings, Logs). Spec: `.scratch/diagnostics-token/spec.md`.

Why: diagnosing the OIDC, RomM and song-search problems of 2026-11-07 meant guessing from code, asking the operator to paste log lines and building probe scripts, because an agent cannot look at a running Requestarr. The views exist as superuser-only routes, but reaching them needs a signed-in superuser's browser session, which would also let the holder clear credentials and change roles.

- **A diagnostics token** is created and revoked by a superuser in Settings, Logs (migration 42, `diagnostics_tokens`). `rqd_` plus 32 random bytes, base64url, shown once, stored only as an HMAC-SHA256 under the session secret (`auth.Hash`), with a required expiry, `last_used_at`, `revoked_at` and a name of 1 to 80 characters. At most 5 are active at once.
- **It reads five routes and nothing else:** `GET /admin/logs`, `/admin/status`, `/admin/job-runs`, `/admin/library-connections` and `/admin/secrets`. Each carries `x-token-readable: true` in `api/openapi.yaml` and a marker in the route table; a route without the marker, and every method but `GET`, answers 401 to a token, and a token cannot manage tokens. `TestTokenReadableRoutesAreExactlyThese` pins the list so a new route is never token-readable by accident. Only `Authorization: Bearer rqd_...` counts: never a query parameter or cookie, and a request with the header is judged as a token request only.
- **Who may use it:** only while its creator is an enabled superuser who can sign in (checked on every request) and it is neither expired nor revoked. Revoking, expiry and demotion take effect on the next request.
- **Abuse and audit:** failed attempts are throttled per client address with the sign-in throttle; a token is limited to 60 requests a minute; `last_used_at` is written at most once a minute. Create and revoke write value-free audit rows with the actor. A refused use logs a warning with a fixed reason, never the value; successful reads are not logged.
- **Client:** `scripts/agent/requestarr-diag.sh status|logs|runs|connections|secrets` reads `diag.url` and `diag.token` from `~/.config/requestarr-probe/` (token file mode 600 and directory mode 700 enforced, as `romm-probe.sh`), gives the token to `curl` on a private descriptor, redacts it from output, and prints one readable line per fact (`logs` takes `--level` and `--after`). `scripts/agent/requestarr-diag-test.sh` tests it against a local fake server with a canary token and runs in `scripts/check.sh`. The operator guide is `docs/DIAGNOSTICS_TOKEN.md`.
- **Answers to the open questions (2026-11-08):** (1) `GET /admin/secrets` and `GET /admin/library-connections` are token-readable: they show where each credential comes from and what is missing, never a value. (2) Lifetime: 30 days by default, 90 at most. (3) A token cannot read `GET /admin/users`, even with emails removed.
- **Not decided here:** an MCP server was decided against building first; if wanted it is a thin wrapper over the same five routes. No write or action (including starting a sync) is available through a token, and there are no per-token scopes.
