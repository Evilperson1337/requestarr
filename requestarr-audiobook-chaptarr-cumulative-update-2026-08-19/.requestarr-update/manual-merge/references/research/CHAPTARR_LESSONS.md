# Chaptarr lessons for Requestarr

Chaptarr is a Readarr-derived library/acquisition manager focused on books and audiobooks. Requestarr does **not** copy its acquisition responsibilities. It is useful as a domain and metadata/matching reference.

## Lessons to retain

1. **Audiobook editions are materially distinct manifestations.** Preserve ASIN/storefront region, narrator(s), publisher, language, runtime, release date, and abridged/dramatized/full-cast information when providers expose it. A title+author pair alone is not sufficient to prove that two audiobook editions are equivalent.
2. **Narrator-aware reconciliation matters.** Exact ASIN/external-ID matches are strongest. A normalized title+author+narrator match may be high confidence. Title+author without edition evidence is only a possible match and must not silently mark a request Available.
3. **Matching provenance should be first-class.** Record the method, confidence, and bounded evidence for every availability match. Exact/high matches may auto-fulfill; possible matches are for admin review.
4. **Metadata providers should be replaceable and composable.** V1 uses Audnexus as the preferred audiobook metadata provider, but the provider seam should allow fallback/enrichment later rather than making one service structurally irreplaceable.
5. **Work -> manifestation is a future model, not a V1 requirement.** Preserve edition identity now so ebook V2 can later relate ebook and audiobook manifestations to a broader canonical work without rewriting existing audiobook requests.

## Explicit non-lessons

Do not import Chaptarr's download clients, indexers, quality profiles, root folders, renaming, conversion, upgrade policies, monitored-author behavior, or acquisition/download statuses. Audiobookshelf remains Requestarr's sole V1 audiobook availability authority.
