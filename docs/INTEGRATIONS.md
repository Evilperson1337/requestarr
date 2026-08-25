# Integration Contracts

## Metadata providers

### TMDB
Movie and TV identity/metadata. V1 accepts canonical TMDB movie/TV URLs and extracts provider IDs. Prefer bearer API read token configuration.

### IGDB
Game identity, cover metadata, and supported platforms. Authentication uses Twitch/IGDB credentials. Platform lists in the request modal come from the resolved game payload and are normalized to stable Requestarr platform keys.

### MusicBrainz
Album requests target release groups; song requests target recordings. Configure an application identity/contact and respect MusicBrainz rate/use requirements. Representative release artwork is presentation metadata only and must not alter recording identity.

### Planned Audiobook metadata: Audible + Audnexus
No audiobook adapter is currently implemented. The approved future resolver accepts Audible links and uses ASIN plus normalized storefront region for identity. Audnexus is metadata/enrichment only and must remain behind a replaceable provider seam; it is never an availability source. Preserve provider-native artwork and edition/narrator details when exposed.

## Availability sources

### Plex / Jellyfin
Authoritative for enabled movie/TV/music library types. Synchronize external IDs and source deep links where possible.

### Navidrome / OpenSubsonic
Authoritative for enabled music. Normalize song/album identity to MusicBrainz IDs when present and retain fallback metadata for confidence matching.

### RomM
Authoritative for games. Preserve platform identity and game metadata-provider IDs when exposed. A request with explicit acceptable platform keys is available when any matching platform instance exists.

### Audiobookshelf snapshot foundation
Phase 5A implements only a local, fixture-backed normalization and complete-snapshot persistence foundation for future Audiobookshelf inventory. It has no live HTTP client, endpoint assumptions, setup/configuration, scheduler, reconciliation invocation, or credentials. A later increment must add those integrations before Audiobookshelf can establish availability. It remains the planned sole authoritative audiobook source; automatic fulfillment will be limited to exact/high-confidence, provenance-recorded matches, while possible edition/narrator matches require review. Chaptarr is research-only and must not be added as a runtime, availability, or acquisition integration.

## Apprise API

Requestarr integrates with Apprise API as a notification gateway rather than implementing dozens of destination-specific services. Support a stateful/config-key mode first; optional stateless URL mode can be evaluated later. All notification attempts are persisted as deliveries and retried according to bounded policy.

Reference: `references/source/manifest.yaml` includes upstream Apprise and Apprise API repositories.

## OIDC / Keycloak

Use OIDC discovery rather than Keycloak-specific endpoints. Keycloak is the required compatibility/test target, not a hard-coded protocol variant. Requested scopes and the groups claim are separately configurable; do not assume a claim name automatically implies an OAuth client scope. Group-to-role mappings are application settings.
