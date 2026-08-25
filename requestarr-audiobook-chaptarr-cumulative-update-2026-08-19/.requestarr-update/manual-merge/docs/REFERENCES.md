# Project References

Requestarr learns from existing projects without making them runtime dependencies. Reference source is for architecture/domain/UX study only; licensing and attribution remain independent.

## Primary UX references

### Seerr
Primary overall consumer UX reference: discovery-first catalog presentation, availability separate from request/approval state, partial availability, simple card badges, media-library awareness, request management, and a UI that answers “can I request/watch this?” rather than exposing backend mechanics. Long term, Requestarr should feel catalog-first in the same way while supporting more media families.

### Libreseerr
Primary secondary UX reference for carrying Seerr-like interaction into book/audiobook domains. Useful lessons: search/detail/request separation, media-format presentation, shared requests/status surfaces, and proof that Seerr interaction language works outside movies/TV. Do **not** copy its acquisition-specific quality-profile/root-folder/download states into Requestarr.

## Audiobook/domain references

### Shelfmark
Reference for book/audiobook request UX, metadata-provider abstraction, OIDC patterns, cover/metadata caching, and separating canonical metadata search from downstream source/release handling. Its acquisition functionality is outside Requestarr scope.

### Listenarr
Primary audiobook-domain reference for Audible-centric metadata, narrator/series/runtime fields, direct Audible search/identity, and audiobook-specific collection modeling. Requestarr borrows identity/domain lessons only, not acquisition/download behavior.

### Audnexus
Preferred V1 audiobook metadata/enrichment reference. Audible ASIN + storefront region are treated as high-value edition identifiers when available.

### Audiobookshelf
V1 audiobook inventory/source-of-truth reference. Requestarr asks Audiobookshelf “do we have it?” and does not use it as the canonical external catalog.

### Readarr / Bookshelf
Book-domain and refresh architecture lessons plus an explicit caution: central metadata dependencies can become a systemic failure point. Readarr is retired; Requestarr must keep provider adapters replaceable and cache normalized catalog data locally. Bookshelf is useful for post-Readarr metadata-provider evolution. Ebooks remain V2.

## Other media/engineering references

### Radarr / Sonarr / Lidarr
Engineering references for persistent local state, fast UI reads, background command/job execution, refresh/rescan separation, event-driven updates, incremental reconciliation, operational status, and resilience around slow external services. Lidarr is additionally useful for artist/album/recording identity challenges.

### GG Requestz
Primary game-request reference: IGDB integration, direct-provider search patterns, dynamic platform metadata, RomM integration, caching/progressive loading, and event/webhook concepts.

### RomM
Game inventory/source-of-truth reference, especially game/platform representation and IGDB-enriched identity.

### Apprise / Apprise API
External notification gateway reference.

### Keycloak
Required V1 OIDC compatibility/test target.

See `references/source/manifest.yaml` and `references/source/README.md`. Never copy reference source into Requestarr merely for convenience; consult licenses before adapting any implementation detail.

### Chaptarr
Audiobook/book domain and matching reference. Use it to study multi-edition coexistence, narrator/publisher-aware audiobook identity, metadata aggregation/provider architecture, and file/catalog matching. It is **not** a Requestarr UX or runtime dependency; its acquisition, indexer, quality-profile, root-folder, conversion, and monitoring concerns remain out of scope. See `references/research/CHAPTARR_LESSONS.md`.
