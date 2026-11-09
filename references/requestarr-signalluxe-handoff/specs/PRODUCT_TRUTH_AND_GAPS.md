# Product truth vs. visual speculation

## Confirmed from supplied screenshots / user's direction
- One consolidated request app for **movies, TV, music, games, audiobooks**; no acquisition layer.
- Dark mode required; preserve media artwork/card paradigms.
- Primary nav presently shows Request, Library, Requests, Issues, Notifications, Settings. **Add Logs**.
- Request: search by title or pasted reference links; selectable categories; includes singles/EPs option; recent requests and newly-added content from multiple libraries.
- Library: media type tabs, title search, source-library filter, multi-column artwork grid, load more.
- Requests: tabs for All, Requested, Approved, Available, Denied, Cancelled; search, type and sort; admin actions (Approve, Deny, Cancel, Notify Me), My Requests.
- Issues: Open, In progress, Resolved, Closed, All filters; issue item with reporter count and status.
- Settings integrations: media sources Plex, Jellyfin, RomM, Navidrome, Audiobookshelf; metadata TMDB, MusicBrainz, IGDB; notifications via Apprise. Other Settings tabs Authentication, Activity, Matches, Users, Logs.
- Product state from one captured instance only; don't hardcode metrics, media titles, services or statuses.

## Proposed / requires discovery in source
- Logs page: available events, access control, pagination, filtering, redaction, retention, export, streaming if supported.
- Standalone Notifications screen (not supplied), notification item actions and preferences.
- Request detail/preview overlay and actual search result interaction.
- Issue details/report form and permissions.
- Per-role visibility and actions; underlying roles, SSO/OIDC, admin capabilities.
- New dashboard counts / recommendation or trending algorithms.
- Whether status/actions appear for all media types; source-sync operations.

## Explicit corrections to generated mockups
- **Remove Radarr, Sonarr, downloader, import, and acquisition examples** from Logs. Replace with plausible Requestarr service logs such as Plex sync, RomM sync, metadata lookup, webhook delivery, authentication, issue updates, queue state changes. Use only events that truly exist in the source.
- Concept Request home labels 'Trending Requests' but original page shows recently added library items. Do not relabel actual library data as trending without a real definition and backend source.
- Some concept Settings cards inaccurately mark not-configured integrations as connected. Render actual states from backend.
- Concept stats (e.g. 1,248 items) and users/timestamps are fabricated placeholders, not target values.
- Do not add Search/Approve buttons or list actions where authorization/server support is absent.
- Design images occasionally place content into generic hero layouts; prioritize browsing density and accessible readability over literal spacing.
