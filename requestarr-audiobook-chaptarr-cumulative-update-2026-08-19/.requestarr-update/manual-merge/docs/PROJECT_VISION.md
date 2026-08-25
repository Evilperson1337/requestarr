# Project Vision

Requestarr is a shared, self-hosted request board for a household or trusted community's media libraries. It unifies movies, television, music, games, and audiobooks behind one simple mental model without becoming another acquisition system.

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

## Product evolution

V1 is deliberately link-first. A user provides a canonical provider link, Requestarr resolves and normalizes it, then uses the same universal item/request interaction used for existing requests and available items.

The long-term UX target is a Seerr/Libreseerr-style discovery experience: catalog browsing and search overlaid with simple local state (`Available`, `Requested`, `Approved`, `Request`, `Notify Me`). Native discovery is a later phase and must feed the same provider-neutral catalog/item modal rather than introduce a second request workflow.

Audiobooks are included in V1 because the domain has a strong metadata + inventory pairing: Audnexus/Audible identity answers what the audiobook is, while Audiobookshelf answers whether it exists. Ebooks are deferred to V2 because canonical work/edition metadata and availability mapping need a separate design decision.
