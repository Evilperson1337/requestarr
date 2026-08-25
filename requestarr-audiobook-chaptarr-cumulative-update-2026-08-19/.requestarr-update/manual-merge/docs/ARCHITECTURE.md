# Architecture

## Architectural style

Requestarr should begin as a modular monolith. A single Go process exposes the HTTP API, serves the production UI, runs background jobs, and writes PostgreSQL. External services remain adapters behind narrow interfaces. Do not prematurely split workers/microservices.

## Modules

- **auth** — OIDC discovery/login/callback/session/role mapping.
- **metadata** — provider URL recognition and normalization.
- **catalog** — cached canonical item records.
- **requests** — request lifecycle and subscriptions.
- **inventory** — normalized library snapshots.
- **reconcile** — matches inventory changes to active requests.
- **notifications** — event fan-out to in-app + Apprise.
- **jobs** — persistent background task coordination.
- **setup** — first-run draft/validation/finalization.
- **web/API** — transport only; no domain policy buried in handlers.

## Fast-path principle

Routine UI requests should primarily read PostgreSQL. External API calls belong in URL resolution, explicit connection tests, and background refresh/sync jobs. Known stale data is generally preferable to blocking the UI.

## Sync and reconciliation

```text
Source sync -> normalized changes -> inventory transaction -> InventoryChanged event
                                                |
                                                v
                                    targeted reconciliation
                                                |
                          status transition if satisfied
                                                |
                              RequestAvailable event
                                                |
                        subscribed users -> notifications
```

A source that is unreachable must fail its sync without generating removals. True removal detection occurs only after a successful authoritative enumeration/delta cycle.

## Matching hierarchy

Prefer stable external IDs over title matching.

- Movie/TV: TMDB first; retain IMDb/TVDB identifiers when available.
- Music album: MusicBrainz release-group.
- Music song: MusicBrainz recording.
- Games: IGDB canonical game + platform matching.
- Audiobooks: Audnexus-normalized Audible edition identity, preferring ASIN + Audible region; Audiobookshelf is the authoritative availability source.
- Fallback title/year matching must record confidence/reason and be reviewable.

## Game fulfillment

For an IGDB game request:
- `Any = true` means any RomM platform instance of the canonical game satisfies it.
- Explicit platform keys are a set of acceptable alternatives.
- Presence of one matching selected platform satisfies the request.
- `Any` and explicit selections are mutually exclusive.

## Audiobook identity and reconciliation

Audiobooks are V1 but remain provider-neutral inside the domain. Audible product URLs are input identifiers; the Audnexus adapter enriches and normalizes them. Persist normalized catalog metadata so ordinary request/history pages never depend on a live Audnexus request.

Matching priority is intentionally conservative:

1. exact ASIN with compatible Audible region when present;
2. alternate stable identifiers exposed by both catalog and Audiobookshelf;
3. ISBN when trustworthy for the audiobook edition;
4. high-confidence normalized title + author + narrator fallback, marked with match evidence/confidence and never silently treated as exact if ambiguous.

Narrator is not merely decoration: different narrations may represent meaningfully different audiobook editions. The inventory adapter should retain narrator/series/raw provider identifiers in normalized metadata even when exact ASIN matching is possible.

## Future discovery boundary

Seerr and Libreseerr are presentation references for the post-V1 discovery layer. Discovery providers may query external catalogs concurrently and progressively, but results must normalize into the same `CatalogItem`/resolved-item presentation used by link resolution. Request, availability, and subscription state are always joined from local PostgreSQL state; a slow discovery provider must not make the shared Requests page or known-item pages slow.

## Reconciliation evidence and confidence

Inventory matching is explainable state, not a hidden boolean. A reconciliation result records `match_method`, `confidence`, bounded safe evidence, the source, and the inventory item when known. `exact` and `high` matches are eligible to automatically satisfy requests; `possible` matches must remain unresolved and be surfaced for administrative review.

Audiobook matching should normally rank evidence as: exact ASIN + Audible region; other shared stable external identifiers; normalized title + author + narrator; then title + author as review-only evidence. A narrator mismatch, dramatized/full-cast edition, or other materially distinct manifestation must not be silently collapsed merely because title and author match.

The same evidence model applies to other media (for example TMDB IDs, MusicBrainz recording IDs, and IGDB game+platform matches) so the reconciliation system stays media-neutral.

## Future work and manifestations

V1 does not implement ebooks or a general publishing-work graph. Audiobook catalog records must nevertheless preserve edition-significant identifiers/metadata and avoid assumptions that prevent a later `work -> manifestation` relation. Ebook V2 may relate multiple ebook and audiobook manifestations to one broader canonical work without changing the V1 requester-facing workflow.
