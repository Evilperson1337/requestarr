# Prompt 11 — Audiobook V1 (future implementation gate)

Do not begin this prompt until a separately approved implementation task follows the Audiobook V1 policy in [D019](../docs/DECISIONS.md#d019--audiobook-v1-policy-and-future-ebook-compatibility-2026-08-21). The current repository does not implement audiobook support.

Implement only the approved vertical slice: link-first Audible identity using ASIN plus normalized storefront region; Audnexus metadata/enrichment behind provider-neutral, replaceable seams; Audiobookshelf as the sole availability authority; provider-native artwork; and edition/narrator-aware matching provenance. Exact/high-confidence matches may automatically fulfill; possible matches must be review-only. Add bounded external clients, sanitized deterministic fixtures, failure-path tests, and append-only migrations where required.

Do not add ebook requests or a canonical work hierarchy (V2), Chaptarr runtime dependencies, or any acquisition, download, indexer, import, rename, quality-profile, root-folder, or library-management behavior. Preserve D018, encrypted setup/configuration decisions, and every existing external-client safety rule.
