# Project Vision

Requestarr is a shared, self-hosted request board for a household or trusted community's media libraries. It unifies movies, television, music, and games behind one simple mental model without becoming another acquisition system.

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

Requestarr does not own:
- downloading/acquisition;
- torrent/Usenet/indexer clients;
- quality profiles or root folders;
- file import/rename/organization;
- game acquisition/install automation;
- *arr orchestration.
