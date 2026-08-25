# Prompt 12 — Discovery Preparation (post-V1, no discovery implementation yet)

Use Seerr as the primary UX reference and Libreseerr as the secondary audiobook/book adaptation reference. Refactor only where necessary so URL resolution, future search results, discovery rails, and similar-item links can all normalize into one provider-neutral catalog presentation and reuse the existing item/request modal.

Do not implement external search/discovery in this prompt. Establish component/domain boundaries, tests, and documentation that ensure future provider concurrency/progressive results cannot make Requests or known-item rendering depend on live external APIs. Preserve media-aware artwork ratios and simple user-facing states (`Available`, `Requested`, `Approved`, `Request`, `Notify Me`).

Preserve the distinction between discovery/catalog identity and reconciliation evidence. Discovery results must not infer availability from fuzzy title matches; availability overlays come from persisted inventory reconciliation, including its confidence/provenance rules. Chaptarr is not a discovery UX reference.
