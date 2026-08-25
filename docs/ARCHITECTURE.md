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

The approved audiobook vertical has a local Phase 5A snapshot foundation only: validated complete snapshots, fixture-backed Audiobookshelf normalization, and local PostgreSQL persistence. It has no live Audiobookshelf API or configuration/runtime wiring. A later increment will add an Audible ASIN-plus-normalized-region resolver and a provider-neutral metadata seam with Audnexus as initial enrichment. Audiobookshelf—not the metadata adapter—will be the only availability authority once its deferred integration is implemented. Chaptarr is not an adapter or dependency.

## Fast-path principle

Routine UI requests should primarily read PostgreSQL. External API calls belong in URL resolution, explicit connection tests, and background refresh/sync jobs. Known stale data is generally preferable to blocking the UI.

## Sync and reconciliation

The current reconciliation foundation accepts one request, library source, and
inventory candidate at a time. It stores bounded normalized evidence plus an
append-only activation/deactivation audit event, without mutating the request
workflow status. Exact and high-confidence evidence can yield a separate
availability-satisfied domain outcome; possible and narrator/edition-conflicted
matches remain review-only. A future Phase-5 inventory adapter owns durable
availability projection after it has supplied authoritative local inventory.

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
- Planned audiobooks: exact Audible ASIN plus normalized storefront region first; bounded edition/narrator-aware matching may be high confidence only with recorded evidence. Possible matches are review-only.
- Fallback title/year matching must record confidence/reason and be reviewable.

Future audiobook persistence must retain edition identity without requiring a work hierarchy. Ebook V2 may later relate ebook and audiobook manifestations to a canonical work.

## Game fulfillment

For an IGDB game request:
- `Any = true` means any RomM platform instance of the canonical game satisfies it.
- Explicit platform keys are a set of acceptable alternatives.
- Presence of one matching selected platform satisfies the request.
- `Any` and explicit selections are mutually exclusive.
