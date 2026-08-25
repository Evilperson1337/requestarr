# Audiobook and Chaptarr research notes

## Status and provenance

This is research-only material, not a dependency, implementation specification, or source import. It is an editorial synthesis of the documentation supplied in the local `requestarr-audiobook-chaptarr-cumulative-update-2026-08-19` package, reviewed on 2026-08-21. The package was not applied, and no source code, generated patch, archive content, or third-party source tree was copied into this repository.

The governing product policy is [D019](../../docs/DECISIONS.md#d019--audiobook-v1-policy-and-future-ebook-compatibility-2026-08-21). Existing reference licensing and provenance guardrails in [the source-reference README](../source/README.md) continue to apply. In particular, Chaptarr must be treated as a domain/matching research input only; it is not a Requestarr runtime dependency and no GPL/AGPL implementation may be copied.

## Retained conclusions

- Treat audiobook editions as manifestations: Audible ASIN plus normalized storefront region is the planned initial identity, while narrator, publisher, language, runtime, release date, abridged state, and presentation form may materially affect equivalence.
- Keep metadata and availability separate. Audnexus is planned metadata/enrichment only; Audiobookshelf is planned as the sole V1 availability authority.
- Require explainable, bounded match provenance. Exact identifier and defensible high-confidence matches may fulfill automatically; possible title/author or incomplete-edition matches require review.
- Keep provider adapters replaceable. A future fallback must enrich the same provider-neutral model rather than leak a vendor-specific model across the domain or UI.
- Preserve provider-native artwork. Artwork is presentation metadata and not fulfillment evidence.
- Ebooks and a work-to-manifestation relation remain V2. Preserve V1 edition identity without prematurely imposing that hierarchy.

## Explicit non-adoption

Requestarr does not adopt Chaptarr acquisition, indexer, downloader, import, root-folder, renaming, conversion, quality-profile, monitoring, or upgrade behavior. It also does not adopt a Chaptarr runtime client or configuration surface.
