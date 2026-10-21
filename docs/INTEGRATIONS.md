# Integration Contracts

## Metadata providers

### TMDB
Movie and TV identity/metadata. V1 accepts canonical TMDB movie/TV URLs and extracts provider IDs. Prefer bearer API read token configuration.

### IGDB
Game identity, cover metadata, and supported platforms. Authentication uses Twitch/IGDB credentials. Platform lists in the request modal come from the resolved game payload and are normalized to stable Requestarr platform keys.

### MusicBrainz
Album requests target release groups; song requests target recordings. Configure an application identity/contact and respect MusicBrainz rate/use requirements. Representative release artwork is presentation metadata only and must not alter recording identity.

## Availability sources

### Jellyfin (operational V1 slice)

Set both `REQUESTARR_JELLYFIN_URL` and `REQUESTARR_JELLYFIN_API_KEY` to enable
the optional integration. The URL must be an origin (no path/query/fragment or
credentials); HTTP is permitted only in development, HTTPS is required otherwise.
This operator-configured URL is trusted configuration, not a security boundary:
an operator can direct it to private HTTP networks, and the integration does not
prevent requests to loopback, link-local, or other internal addresses. Treat this
as a private-network request risk; do not make the URL user-configurable or
accept it from untrusted tenants. Sync runs
once during startup in a background goroutine and every 15 minutes thereafter.
It imports movie/series TMDB identifiers and records exact catalog availability.
Only a fully successful paginated snapshot is persisted. Failures and incomplete
runs update health only and preserve absence evidence. Exact TMDB movie/TV
matches begin an absence streak on the first qualifying complete snapshot; a
second distinct complete absence at least 24 hours later can mark a previously
available catalog item missing. Empty snapshots are excluded from negative
evidence by default; enabling them requires an explicit persisted operator
acknowledgement in `jellyfin_reconciliation_settings.allow_empty_snapshots`.
When an item returns, its streak is cleared and positive evidence restores
availability. Enabled authoritative evidence from another source prevents a
negative Jellyfin result from changing aggregate availability. Availability
events and notifications are committed atomically with the transition, and
request lifecycle status is never changed. The administrator-only `GET /api/v1/admin/sources/jellyfin/status`
endpoint reports source configuration/enabled state, sync timestamps, and a
bounded safe error message. It excludes the server URL, API key, and inventory
contents. Request list items expose catalog availability separately from request
lifecycle and include the latest positive evidence observation time. Jellyfin
outages do not cause availability to be marked missing.

Plex and the remaining Jellyfin media types are not operationally integrated.

### Navidrome / OpenSubsonic
Authoritative for enabled music. Normalize song/album identity to MusicBrainz IDs when present and retain fallback metadata for confidence matching.

### RomM
Authoritative for games. Preserve platform identity and game metadata-provider IDs when exposed. A request with explicit acceptable platform keys is available when any matching platform instance exists.

## Apprise API

Requestarr integrates with Apprise API as one operator-managed global notification route. Configure `REQUESTARR_APPRISE_URL` and `REQUESTARR_APPRISE_CONFIG_KEY` together; delivery is disabled when both are empty. URL must use HTTPS except localhost in development. A bounded background worker sends one outbox event per source event, independent of in-app subscriptions. In-app notifications remain per-user and are created only for request subscribers. The former `notification_preferences` and user-scoped Apprise rows from migration 000010 are deprecated and are not used by the global sender.

Downgrading migration `000010` is data-lossy: it drops notification preferences and columns containing notification content, read state, event keys, and delivery scheduling state. Export required data before reverting this migration; rollback cannot preserve those fields.

Reference: `references/source/manifest.yaml` includes upstream Apprise and Apprise API repositories.

## OIDC / Keycloak

Use OIDC discovery rather than Keycloak-specific endpoints. Keycloak is the required compatibility/test target, not a hard-coded protocol variant. Requested scopes and the groups claim are separately configurable; do not assume a claim name automatically implies an OAuth client scope. Group-to-role mappings are application settings.
# Availability core status

The current availability types, evidence schema, and exact-identifier matching
are domain/reconciliation foundations only. No library source is currently
configured through runtime settings or the UI, so the application does not
provide operational availability sync or availability badges. Title/year
matches are suggestions, never automatic confirmations. Failed or incomplete
source runs cannot establish absence; this core intentionally does not infer
missing status because enabled-source coverage is not yet modeled. Request
lifecycle status remains independent from availability.
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
