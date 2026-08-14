# Project References

Requestarr learns from several existing projects without making them runtime dependencies.

## Seerr
Primary reference for consumer-facing request UX, availability distinct from approval, partial availability, media-library synchronization, simple statuses, permissions, and request management.

## Radarr / Sonarr / Lidarr
Engineering references for persistent local state, fast UI reads, background command/job execution, refresh/rescan separation, event-driven updates, incremental reconciliation, status/health visibility, and robust behavior around slow external services. Lidarr is additionally useful for artist/album/track identity challenges.

## GG Requestz
Primary game-request reference: IGDB integration, direct provider search patterns, dynamic platform metadata, RomM integration, OIDC, caching/progressive loading, and event/webhook concepts.

## RomM
Game inventory/source-of-truth reference, especially game/platform representation and IGDB-enriched library identity.

## Apprise / Apprise API
External notification gateway reference.

## Keycloak
OIDC compatibility and test environment reference.

These repositories have independent licenses. See `references/source/manifest.yaml` and `references/source/README.md` before consulting or copying code.
