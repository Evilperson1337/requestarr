# V1 Scope

## Required

- Go service + React/TypeScript web UI + PostgreSQL.
- First-run Get started checklist (computed from live state, dismissible) after the first-admin claim; configuration itself lives in Settings (D038).
- OIDC-only normal authentication; Keycloak tested and documented.
- Roles/capabilities for viewer/requester/admin.
- URL resolver registry:
  - TMDB movie + TV URLs;
  - MusicBrainz release-group/recording URLs;
  - IGDB game URLs;
  - Audible product URLs or a bare ASIN for audiobooks (D031).
- Universal resolved-item modal.
- One request row per canonical catalog item.
- Shared Requests page with search/filter/sort and My Requests quick filter.
- Original requester automatically subscribed.
- Notify Me/unsubscribe for other users.
- Game platform selection dynamically built from IGDB: `Any` first, or multiple explicit platforms; OR semantics.
- Request states: requested, approved, denied, available, cancelled.
- Manual admin state actions with audit events.
- Metadata cache/local catalog records.
- Library sources: Plex, Jellyfin, Navidrome/OpenSubsonic, RomM, Audiobookshelf (audiobooks only).
- Background source sync into normalized inventory.
- Automatic request reconciliation and availability notification.
- In-app notifications and Apprise API integration.
- Settings and operational status for providers/libraries/jobs/logs.
- Preserve artwork shape by media type.

## Explicitly out of V1

- Audiobook sources other than Audiobookshelf (BookOrbit is not supported), ISBN matching, and audiobook Issue kinds.
- Catalog discovery, trending and recommendations. (Name search is V1.x, below.)
- Radarr/Sonarr/Lidarr runtime integration.
- Acquisition/download automation of any kind.
- Multiple request/vote counts for the same item.
- Individual per-user private request queues.
- Complex quotas/multi-level approvals.
- Game DLC/update/edition acquisition workflows.
- Mobile push implementation outside Apprise.

## V1.x (after release)

- Name search as a front door to the URL resolver (D032): results point at items the resolver already accepts, search never writes, and it ships by provider in this order: TMDB, IGDB, MusicBrainz, Audible catalog. Spec: `.scratch/name-search/spec.md`.
