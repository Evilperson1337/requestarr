# Project Vision

Requestarr is a shared, self-hosted request board for a household or trusted community's media libraries. It unifies movies, television, music, games, and planned audiobooks behind one simple mental model without becoming another acquisition system.

## Product promise

**Tell Requestarr what the library should have; Requestarr tells everyone when the library has it.**

A request is shared state. The user who creates it is its originator, not its owner. Other users may subscribe with **Notify Me**, but they do not create duplicate requests or votes.

## Boundaries

Requestarr owns:
- identity/session/authorization;
- metadata URL resolution;
- request approval and history;
- shared request visibility;
- personal request subscriptions;
- normalized inventory snapshots;
- availability reconciliation;
- in-app and Apprise notifications;
- first-run configuration and operational health.

Audiobook support is policy-only at present. The planned V1 workflow is Audible ASIN plus normalized storefront-region identity, Audnexus metadata/enrichment, and Audiobookshelf-observed availability. It treats edition and narrator as meaningful identity evidence; ebooks and a broader work model remain V2 work.

Requestarr does not own:
- downloading/acquisition;
- torrent/Usenet/indexer clients;
- quality profiles or root folders;
- file import/rename/organization;
- game acquisition/install automation;
- *arr orchestration.
- Chaptarr runtime integration and all audiobook acquisition/library-management responsibilities.
