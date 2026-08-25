# Prompt 00 — Master Context

Use this prompt at the start of every fresh AI implementation session.

Read `AGENTS.md`, `docs/PROJECT_VISION.md`, `docs/V1_SCOPE.md`, `docs/DECISIONS.md`, `docs/ARCHITECTURE.md`, `docs/UI_UX.md`, and `docs/SECURITY.md`. Treat them as authoritative. Inspect the current repository and tests before changing code. Audiobooks are V1 using Audible/Audnexus metadata identity and Audiobookshelf availability; ebooks are V2. Long-term discovery UX should learn from Seerr/Libreseerr while preserving the link-first V1 and universal item modal. Chaptarr is a domain/matching reference for edition-aware audiobook identity and explainable reconciliation; it is not a runtime or acquisition dependency. Do not introduce acquisition/download behavior. Do not copy reference-project source code. Implement the smallest production-quality increment requested, preserve module boundaries, add tests, run the documented checks, and summarize changed behavior plus remaining risks.
