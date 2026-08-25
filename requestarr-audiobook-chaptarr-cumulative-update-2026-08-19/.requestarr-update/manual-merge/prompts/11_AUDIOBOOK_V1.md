# Prompt 11 — Audiobook V1

Implement audiobook V1 as an extension of the existing Requestarr pipeline. Read D019-D026, `references/research/AUDIOBOOKS_AND_DISCOVERY.md`, and `references/research/CHAPTARR_LESSONS.md` first.

Required behavior:
- add `audiobook` media support through the forward-only migration;
- accept Audible product URLs and resolve/enrich through an Audnexus adapter;
- normalize ASIN, Audible region, authors, narrators, series/position, runtime, language, publisher, release date, abridged state, edition/presentation type when known, cover, and alternate stable IDs;
- cache normalized catalog data so existing views/reconciliation do not depend on live Audnexus;
- add Audiobookshelf as the only V1 audiobook availability authority;
- record every reconciliation association using the shared match-evidence model;
- prefer exact ASIN+region/shared external IDs; permit only unambiguous high-confidence title+author+narrator fallback matches to auto-fulfill;
- treat title+author-only, narrator conflicts, and materially different productions as `possible`/review-only rather than automatically Available;
- integrate audiobook provider/library configuration into setup + settings;
- support ordinary shared request, approval, Notify Me, Available, and Apprise/in-app notification flows;
- preserve source-native audiobook artwork ratio;
- keep provider boundaries capable of future fallback/enrichment without implementing multi-provider consensus now;
- keep the domain compatible with a future work -> manifestation relationship without implementing ebook/work hierarchy in V1.

Do not add ebook support, Chaptarr runtime integration, acquisition, download progress, quality profiles, root folders, native global discovery, or Plex audiobook authority. Add provider fixtures, source-outage tests, exact-ID/fallback/ambiguity matching tests, migration tests, and end-to-end audiobook request->availability coverage.
