# Integration Contracts

## Metadata providers

### TMDB
Movie and TV identity/metadata. V1 accepts canonical TMDB movie/TV URLs and extracts provider IDs. Prefer bearer API read token configuration.

### IGDB
Game identity, cover metadata, and supported platforms. Authentication uses Twitch/IGDB credentials. Platform lists in the request modal come from the resolved game payload and are normalized to stable Requestarr platform keys.

### MusicBrainz
Album requests target release groups; song requests target recordings. Configure an application identity/contact and respect MusicBrainz rate/use requirements. Representative release artwork is presentation metadata only and must not alter recording identity.

## Availability sources

### Plex / Jellyfin
Authoritative for enabled movie/TV/music library types. Synchronize external IDs and source deep links where possible.

### Navidrome / OpenSubsonic
Authoritative for enabled music. Normalize song/album identity to MusicBrainz IDs when present and retain fallback metadata for confidence matching.

### RomM
Authoritative for games. Preserve platform identity and game metadata-provider IDs when exposed. A request with explicit acceptable platform keys is available when any matching platform instance exists.

## Apprise API

Requestarr integrates with Apprise API as a notification gateway rather than implementing dozens of destination-specific services. Support a stateful/config-key mode first; optional stateless URL mode can be evaluated later. All notification attempts are persisted as deliveries and retried according to bounded policy.

Reference: `references/source/manifest.yaml` includes upstream Apprise and Apprise API repositories.

## OIDC / Keycloak

Use OIDC discovery rather than Keycloak-specific endpoints. Keycloak is the required compatibility/test target, not a hard-coded protocol variant. Requested scopes and the groups claim are separately configurable; do not assume a claim name automatically implies an OAuth client scope. Group-to-role mappings are application settings.

## Audiobooks

### Audnexus / Audible

V1 accepts Audible product URLs and resolves the ASIN + storefront region, then uses Audnexus as the preferred audiobook metadata/enrichment adapter. Store normalized metadata locally. Audnexus identifies the audiobook; it never establishes availability.

Normalized audiobook metadata should preserve, when available: title, subtitle, authors, narrators, series + position, ASIN, Audible region, runtime, release date, publisher, language, description, cover URL, and alternate identifiers. ASIN + region are the preferred edition identity. Avoid title-only canonicalization.

The provider adapter must be replaceable: an Audnexus outage may prevent new resolution but must not break rendering of cached catalog items, request state, or reconciliation against existing inventory.

### Audiobookshelf

Audiobookshelf is the V1 authoritative source of truth for audiobooks. Its adapter synchronizes normalized audiobook inventory and source deep links. Prefer exact ASIN/external-ID reconciliation, retain narrator/series metadata for fallback matching, and preserve last-known inventory on incomplete or failed syncs. Do not use Plex as an audiobook authority in V1.

## Ebooks (V2)

Ebooks are intentionally deferred. Potential inventory sources include BookOrbit, Kavita, and Calibre-family systems, but Requestarr must first settle canonical work/edition identity and metadata-provider strategy. Shelfmark/Libreseerr/Readarr/Bookshelf are research references; none should become a V1 runtime dependency.

### Chaptarr (reference only)
Chaptarr is not a runtime integration. It is included in the source-reference manifest for research into audiobook edition/narrator/publisher identity, modular metadata pipelines, and matching behavior. Audiobookshelf remains the sole V1 audiobook availability authority.
