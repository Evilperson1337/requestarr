# Audiobook + discovery research synthesis

## Adopt

- **Seerr:** discovery/catalog presentation overlaid with simple local availability/request state.
- **Libreseerr:** proof that Seerr-style card/detail/request interaction translates to book/audiobook media.
- **Shelfmark:** provider abstraction, audiobook-specific metadata selection, OIDC/caching lessons.
- **Listenarr:** Audible-first audiobook identity fields (author, narrator, series, runtime, ASIN).
- **Readarr:** refresh/library separation and a caution against a single irreplaceable metadata backend.
- **Audiobookshelf:** V1 source of truth for audiobook availability.
- **Audnexus:** preferred V1 audiobook metadata/enrichment provider.

## Explicitly reject

- acquisition/download/indexer/root-folder/quality-profile behavior from Libreseerr/Shelfmark/Listenarr/Readarr;
- downloading/importing/progress states in the requester-facing lifecycle;
- local-password-first authentication patterns; Requestarr remains OIDC-first;
- provider-specific models leaking into UI/domain contracts;
- treating a metadata provider as availability authority.

## Requestarr audiobook V1

Audible URL -> parse ASIN + storefront region -> Audnexus normalize/cache -> universal request modal -> one shared request -> Audiobookshelf inventory sync -> exact ID reconciliation -> Available -> notify requester + manual subscribers.

## Future discovery

Search/discovery is post-V1. Multiple provider searches may execute concurrently and progressively, but each result normalizes into the same catalog presentation and universal item/request modal. Shared Requests and known-item screens remain local-state-first and never wait on discovery providers.

## Chaptarr refinement (2026-08-19)
Chaptarr reinforces that audiobook identity is edition/manifestation-aware: narrator, publisher, release/presentation type and multi-edition coexistence can be significant, not merely decorative metadata. Requestarr therefore records reconciliation match provenance/confidence and only auto-fulfills exact/high-confidence matches. Chaptarr remains a domain/matching reference only; Audiobookshelf remains V1 availability authority and acquisition features stay out of scope. See `CHAPTARR_LESSONS.md`.
