# Integration Contracts

## Metadata providers

### TMDB
Movie and TV identity/metadata. V1 accepts canonical TMDB movie/TV URLs and extracts provider IDs. Prefer bearer API read token configuration.

### Audible (audiobooks, D031)
An audiobook is requested by its ASIN, given bare or as an Audible product link
(`audible.<tld>/pd/[slug/]<ASIN>`; Amazon links are not accepted). Details (title,
authors, narrators, cover, abridged or not, series, length) come from **Audnexus**
(`api.audnex.us`) first and Audible's catalog endpoint second. **Neither is a
documented, contractual API**: Audnexus is a free community service that points new
projects to AudiobookDB, and Audible's endpoint is undocumented, so either can
change or disappear and neither publishes terms. Because of that the ASIN is the
identity and the details are best-effort display data: if both fail the request is
accepted under the placeholder title `Audiobook <ASIN>` and fills in when a later
lookup succeeds; a placeholder never replaces details already stored. One lookup is
made per new request, never when a page renders, and answers are cached.

ASINs belong to a marketplace. A bare ASIN is looked up in the marketplace named by
`REQUESTARR_AUDIBLE_REGION` (`us` when unset; `us ca uk au fr de jp it in es`); an
Audible link's own domain overrides it. If neither marketplace knows the ASIN the
requester is asked to paste the link for the right one. The region is shown but is
never part of the match key, and two ASINs for the same book are two Requests.
AudiobookDB is not used in v1: its free tier requires attribution and forbids
long-term caching of results. See `docs/research/AUDIOBOOK_INTEGRATION.md`.

### IGDB
Game identity, cover metadata, and supported platforms. Authentication uses Twitch/IGDB credentials. Platform lists in the request modal come from the resolved game payload and are normalized to stable Requestarr platform keys.

### MusicBrainz
Album requests target release groups; song requests target recordings. A pasted `/release/` link is accepted but resolved to its release group (a release is one edition of the album), and any other entity is unsupported. Catalog rows created by earlier builds may hold a release or recording filed as an album; they cannot be corrected automatically and should be re-requested from the right link. Configure an application identity/contact and respect MusicBrainz rate/use requirements. Representative release artwork is presentation metadata only and must not alter recording identity.

## Availability sources

### Plex (movies, TV and music)

Set both `REQUESTARR_PLEX_URL` and `REQUESTARR_PLEX_TOKEN` to enable it. The URL
is an origin with the same rules as Jellyfin's (no credentials, path, query or
fragment; HTTPS outside development) and is trusted operator configuration with
the same private-network caveat. The token is sent only in the `X-Plex-Token`
header, never in a URL, and never appears in errors, logs or run history.

Every movie, TV and music library visible to the token is read (photo
libraries are ignored) through the job runner on the same schedule and Sync now
action as Jellyfin. Items are matched to the catalog by TMDB id from Plex's
`Guid` entries; IMDb and TVDB ids are kept as inventory but match nothing yet.
A token that sees no movie, TV or music library is an error, not an empty library, and
every failure (HTTP error, bad token, malformed or truncated page, a later
library failing) discards the whole run: it records health only and removes
nothing. Availability rules are shared with every Source (see Jellyfin below).

Music libraries are read as albums and tracks. Plex tags an album with a
MusicBrainz *release* id and a track with a *release-track* id (older agents in
the top-level `guid`, newer ones in `Guid` as `mbid://`), while a Request's
identity is the release group (album) or recording (song). Before each Plex sync
Requestarr therefore asks MusicBrainz, for up to 20 open music Requests that
have no aliases yet, which releases belong to the release group (album) or which
tracks carry the recording (song), and stores the answers as aliases of the
catalog item (`catalog_item_aliases`). The Plex `mbid` values are then matched
against those aliases, per media type. Lookups are spaced one per second as
MusicBrainz requires, are best-effort (a failure is logged, retried next sync and
never alters inventory), and a Request made after a sync is matched from the next
one. Libraries where Plex exposes only `plex://` identifiers match nothing until
Plex tags them with MusicBrainz ids. These Plex identifier semantics come from
community reports, not official Plex documentation: verify against your server.

Known limit: evidence is only expired by that Source's own successful syncs. If a Source is removed from configuration its existing evidence is not expired yet, so items only it held stay available until evidence expiry for removed or disabled Sources is built.

### Jellyfin (operational V1 slice)

Set both `REQUESTARR_JELLYFIN_URL` and `REQUESTARR_JELLYFIN_API_KEY` to enable
the optional integration. The URL must be an origin (no path/query/fragment or
credentials); HTTP is permitted only in development, HTTPS is required otherwise.
This operator-configured URL is trusted configuration, not a security boundary:
an operator can direct it to private HTTP networks, and the integration does not
prevent requests to loopback, link-local, or other internal addresses. Treat this
as a private-network request risk; do not make the URL user-configurable or
accept it from untrusted tenants. Sync runs
through the job runner (D018) once during startup and every 15 minutes
thereafter, and on demand from Settings.
It imports movie/series TMDB identifiers and records exact catalog availability.
Only a fully successful paginated snapshot is persisted. Failures and incomplete
runs update health only and preserve absence evidence. Exact TMDB movie/TV
matches begin an absence streak on the first qualifying complete snapshot; a
second distinct complete absence at least 24 hours later can mark a previously
available catalog item missing. Empty snapshots are excluded from negative
evidence by default; enabling them requires an explicit persisted operator
acknowledgement in `source_reconciliation_settings.allow_empty_snapshots` (one row per Source).
When an item returns, its streak is cleared and positive evidence restores
availability. Absence is tracked per Source: an item stays available until
every enabled Source covering its media type has completed a sync without it
(D015), so Jellyfin dropping an item never hides it while Plex still has it. Availability
events and notifications are committed atomically with the transition, and
request lifecycle status is never changed. The administrator-only `GET /api/v1/admin/status`
endpoint reports, for every Source, whether it is configured, its latest run,
the time of its last successful snapshot and a bounded safe error message. It
excludes the server URL, API key, and inventory
contents. Request list items expose catalog availability separately from request
lifecycle and include the latest positive evidence observation time. Jellyfin
outages do not cause availability to be marked missing.

Jellyfin covers movies and TV only; music comes from Plex (and later Navidrome) and games from RomM.

### Navidrome / OpenSubsonic

Set `REQUESTARR_NAVIDROME_URL`, `REQUESTARR_NAVIDROME_USERNAME`, and
`REQUESTARR_NAVIDROME_PASSWORD` together to enable the optional music source.
The URL follows the other Source origin rules: no credentials, path, query or
fragment; HTTPS outside development. Use a dedicated read-only account. The
OpenSubsonic salted-token authentication is passed using standard API query
parameters; the clear password is not sent, but intermediaries must not log
request URLs. Redirects are disabled and responses are size-limited.

The adapter reads visible music folders, paginated albums, and each album's
tracks. It declares authority for albums and songs only. MusicBrainz album and
recording IDs are normalized to lowercase `musicbrainz` identifiers when present; items
without IDs retain title/year metadata but do not automatically match catalog
items. Every page and album detail must be fetched successfully before a
complete snapshot is persisted. Failed, malformed, or incomplete syncs update
health only and never remove prior availability evidence. Sync uses the shared
job runner and the same `Sync now` action as other Sources.

### RomM
Authoritative for games only. Set both `REQUESTARR_ROMM_URL` and
`REQUESTARR_ROMM_TOKEN` to enable it (same URL rules, trust caveat and
header-only secret handling as the other Sources). The token is a bearer token
RomM accepts for `Authorization: Bearer`, with at least the `roms.read` and
`platforms.read` scopes. It syncs through the job runner on the same schedule and
Sync now action as the others.

Each ROM is matched to a game Request by its IGDB game id. RomM keeps one ROM per
platform, so evidence and absence streaks are tracked per platform: the platform
is RomM's platform mapped to its IGDB platform id (`igdb:<id>`, the same keys the
request modal offers). A game Request is satisfied by the OR rule (D009): with
explicit platforms, when the game is present on **any** selected platform; with
Any, on any platform at all. A platform RomM cannot map to IGDB (no `igdb_id`)
counts for Any but never for an explicit selection. ROMs whose file is missing
from disk are not evidence. Like Plex, any failure (HTTP error, bad token,
malformed or truncated page, an unknown platform, an older RomM that answers
`/api/roms` with a bare array instead of the paged envelope) discards the whole
run and removes nothing.

The platform fields come from RomM's published API reference; the ROM item
fields used (`id`, `name`, `igdb_id`, `platform_id`, `missing_from_fs`) were not
visible in the docs available when this was built, so decoding is strict and a
mismatch fails the sync visibly. Check your RomM version's `/api/docs` if a sync
reports an invalid response. The fixtures are hand-written to that shape, not
captured from a live server.

### Audiobookshelf
Authoritative for audiobooks only (D031). Set `REQUESTARR_AUDIOBOOKSHELF_URL` (a
server origin, no path) and `REQUESTARR_AUDIOBOOKSHELF_API_KEY`, or save the key
in Settings (same URL rules and trust caveat as the other Sources). API keys need
Audiobookshelf **2.26.0 or later**; older servers are not supported.

Create a dedicated Audiobookshelf user with access to the book libraries only and
`update`, `delete` and `upload` off (a standard `user` account has them off by
default; Audiobookshelf has no stricter read-only type), then create an API key
for it. The key is sent as `Authorization: Bearer` and never in a URL. Requestarr
only issues `GET` requests: it lists libraries, then each **book** library's items
(`minified`, paged), and never triggers a scan, match or edit. The connection
check passes only when the key can see at least one book library.

An item is an audiobook when it has at least one audio track (`numTracks > 0`);
ebook-only items and items Audiobookshelf marks missing or invalid are not
availability. An item is matched to an audiobook Request by its ASIN
(`media.metadata.asin`); items with no ASIN are listed but can only be matched
through the suggested-matches review. Run Audiobookshelf's own Match to fill ASINs.
A server with no readable book library fails the sync rather than reading as an
empty collection.

The response fields come from Audiobookshelf's server code (its in-repo OpenAPI
spec is out of date), and the fixtures are hand-written to that shape, not
captured from a live server. BookOrbit is not supported: it has no machine API
token (`docs/research/AUDIOBOOK_INTEGRATION.md`).

## Apprise API

Requestarr integrates with Apprise API as one operator-managed global notification route. Configure `REQUESTARR_APPRISE_URL` and `REQUESTARR_APPRISE_CONFIG_KEY` together; delivery is disabled when both are empty. URL must use HTTPS except localhost in development. A bounded background worker sends one outbox event per source event, independent of in-app subscriptions. In-app notifications remain per-user and are created only for request subscribers. The former `notification_preferences` and user-scoped Apprise rows from migration 000010 are deprecated and are not used by the global sender.

Downgrading migration `000010` is data-lossy: it drops notification preferences and columns containing notification content, read state, event keys, and delivery scheduling state. Export required data before reverting this migration; rollback cannot preserve those fields.

Reference: `references/source/manifest.yaml` includes upstream Apprise and Apprise API repositories.

## OIDC / Keycloak

Use OIDC discovery rather than Keycloak-specific endpoints. Keycloak is the required compatibility/test target, not a hard-coded protocol variant. Requested scopes and the groups claim are separately configurable; do not assume a claim name automatically implies an OAuth client scope. Group-to-role mappings are application settings.
# Availability core status

Configured runtime Sources persist complete snapshots through the shared
availability pipeline. Exact identifiers establish source/platform evidence;
title/year-only candidates are **suggestions**, never automatic confirmations.
Admins can inspect both titles, years, Source, platform, confidence, and reason
under **Settings → Suggested matches**, then accept or dismiss. Acceptance
adds ordinary evidence and follows the same request/notification path as an
exact match. Dismissal does not change availability and later syncs do not
reopen the same candidate. These are local-state operations, not live provider
lookups. The authenticated admin API is documented in `api/openapi.yaml`.

Failed or incomplete source runs cannot establish absence. Accepted matches
are revalidated on later complete syncs, with the same multi-Source and
two-successful-absences safeguards as exact matches. A successful sync that
omits a pending candidate hides it from review and prevents stale acceptance.
Request lifecycle status remains independent from availability.
# Durable notifications

Approval, denial, and positive availability changes create user-scoped in-app
notification rows in the same PostgreSQL transaction as their source event.
The inbox API is authenticated, and marking an item read also requires the
session CSRF token. The delivery rows are idempotent by event, recipient, and
channel.

Migration 000011 adds a separate global Apprise outbox with lease tokens,
bounded retries, and event-key deduplication. Delivery is at-least-once: a crash
after upstream acceptance but before lease acknowledgement may duplicate a
notification. The sender never logs the configuration key and bounds request
duration and response consumption. Legacy per-user settings remain for
compatibility but are not read. Temporary library failures do not create
availability notifications.
