# V1 Scope

## Required

- Go service + React/TypeScript web UI + PostgreSQL.
- First-run setup wizard with resumable draft configuration.
- OIDC-only normal authentication; Keycloak tested and documented.
- Roles/capabilities for viewer/requester/admin.
- URL resolver registry:
  - TMDB movie + TV URLs;
  - MusicBrainz release-group/recording URLs;
  - IGDB game URLs;
  - Audible audiobook product URLs resolved/enriched through Audnexus.
- Universal resolved-item modal.
- One request row per canonical catalog item.
- Shared Requests page with search/filter/sort and My Requests quick filter.
- Original requester automatically subscribed.
- Notify Me/unsubscribe for other users.
- Game platform selection dynamically built from IGDB: `Any` first, or multiple explicit platforms; OR semantics.
- Audiobook requests with normalized author, narrator, series, runtime, ASIN, Audible region, and provider-native cover metadata when available.
- Request states: requested, approved, denied, available, cancelled.
- Manual admin state actions with audit events.
- Metadata cache/local catalog records.
- Library sources: Plex, Jellyfin, Navidrome/OpenSubsonic, RomM, Audiobookshelf.
- Background source sync into normalized inventory.
- Automatic request reconciliation and availability notification.
- In-app notifications and Apprise API integration.
- Settings and operational status for providers/libraries/jobs/logs.
- Preserve artwork shape by media type.

## Explicitly out of V1

- Native catalog search/discovery/trending/recommendations (planned post-V1 using Seerr/Libreseerr as primary UX references).
- Ebooks and ebook-library sources (BookOrbit/Kavita/Calibre-family); metadata/work-edition strategy is a V2 decision.
- Radarr/Sonarr/Lidarr runtime integration.
- Acquisition/download automation of any kind.
- Multiple request/vote counts for the same item.
- Individual per-user private request queues.
- Complex quotas/multi-level approvals.
- Game DLC/update/edition acquisition workflows.
- Mobile push implementation outside Apprise.

### Audiobook reconciliation safety
Audiobook V1 is edition-aware and explainable. Requestarr records reconciliation method/confidence and automatically marks requests Available only for exact or unambiguous high-confidence matches. Ambiguous title/author matches, narrator conflicts, or distinct dramatized/full-cast editions remain review-only. Chaptarr informs this domain/matching behavior but is not a V1 integration.
