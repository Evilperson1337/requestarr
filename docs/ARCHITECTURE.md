# Architecture

## Architectural style

Requestarr should begin as a modular monolith. A single Go process exposes the HTTP API, serves the production UI, runs background jobs, and writes PostgreSQL. External services remain adapters behind narrow interfaces. Do not prematurely split workers/microservices.

## Modules

- **auth** — OIDC discovery/login/callback/session/role mapping.
- **metadata** — provider URL recognition and normalization.
- **catalog** — cached canonical item records.
- **requests** — request lifecycle and subscriptions.
- **inventory** — normalized library snapshots.
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
- Fallback title/year matching must record confidence/reason and be reviewable.

When no stable identifier matches, a complete inventory sync records reviewable
title/year candidates separately from availability evidence. Candidate lookup
uses an index on media type and normalized title: lower-case, trim, and collapse
punctuation/whitespace into word separators. Titles must match exactly after
normalization, media types must agree, and years must agree when both exist.
A missing year is explicitly lower confidence (`title_only`) with a reason.

Admins review candidates under **Settings → Suggested matches**. Acceptance
records actor/time and normal source/platform evidence in the same transaction
as availability, request events, and notifications. An accepted match is local
to that catalog/library-item pair, not a global provider alias. Later complete
syncs revalidate it and maintain evidence through the ordinary reconciliation
path. If the item disappears or stops matching, ordinary absence safeguards
apply. Exact identifier matches always take precedence.

Dismissal is terminal for the pair and never changes availability. Both review
decisions are idempotent and audited; a conflicting decision returns a conflict.
Only current candidates from the last successful complete snapshot can be
accepted. Failed syncs preserve the last-known view; a successful snapshot that
omits the item makes the candidate stale, including guarded empty snapshots.

## Game fulfillment

For an IGDB game request:
- `Any = true` means any RomM platform instance of the canonical game satisfies it.
- Explicit platform keys are a set of acceptable alternatives.
- Presence of one matching selected platform satisfies the request.
- Evidence is tracked per platform (D021); a platform a Source cannot map to IGDB satisfies Any but never an explicit selection.
- `Any` and explicit selections are mutually exclusive.
