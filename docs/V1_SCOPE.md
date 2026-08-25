# V1 Scope

## Required

- Go service + React/TypeScript web UI + PostgreSQL.
- First-run setup wizard with resumable draft configuration.
- OIDC-only normal authentication; Keycloak tested and documented.
- Roles/capabilities for viewer/requester/admin.
- URL resolver registry:
  - TMDB movie + TV URLs;
  - MusicBrainz release-group/recording URLs;
  - IGDB game URLs.
  - Planned only: Audible audiobook URLs, identified by ASIN plus normalized storefront region; no resolver exists yet.
- Universal resolved-item modal.
- One request row per canonical catalog item.
- Shared Requests page with search/filter/sort and My Requests quick filter.
- Original requester automatically subscribed.
- Notify Me/unsubscribe for other users.
- Game platform selection dynamically built from IGDB: `Any` first, or multiple explicit platforms; OR semantics.
- Request states: requested, approved, denied, available, cancelled.
- Manual admin state actions with audit events.
- Metadata cache/local catalog records.
- Library sources: Plex, Jellyfin, Navidrome/OpenSubsonic, RomM; planned only: Audiobookshelf for audiobooks.
- Background source sync into normalized inventory.
- Automatic request reconciliation and availability notification.
- In-app notifications and Apprise API integration.
- Settings and operational status for providers/libraries/jobs/logs.
- Preserve artwork shape by media type.
- Planned audiobook policy: Audnexus metadata/enrichment, Audiobookshelf-only availability, edition/narrator-aware reconciliation, and exact/high-confidence automatic fulfillment only.

## Explicitly out of V1

- Native catalog search/discovery/trending/recommendations.
- Radarr/Sonarr/Lidarr runtime integration.
- Acquisition/download automation of any kind.
- Multiple request/vote counts for the same item.
- Individual per-user private request queues.
- Complex quotas/multi-level approvals.
- Game DLC/update/edition acquisition workflows.
- Mobile push implementation outside Apprise.
- Ebook requests and a canonical work-to-manifestation model (V2).
- Chaptarr runtime integration or any audiobook acquisition, download, import, or library-management behavior.
