# AI / Coding Agent Guardrails

These instructions apply to any AI agent or automated coding workflow operating in this repository.

## Product boundary

Requestarr is **request + availability + notification software**. It must not acquire media, integrate with download clients, indexers, Usenet/torrent clients, or silently evolve into an *arr orchestrator. Do not add Radarr/Sonarr/Lidarr as runtime dependencies unless a future explicit project decision changes this boundary.

## Accepted V1 decisions

1. Request entry goes through the URL resolver for TMDB, MusicBrainz, IGDB and Audible items. People paste a link (an audiobook may also use its bare ASIN, D031); name search (D032, post-release V1.x) is only a read-only front door that hands the resolver the same link. No other request path, no discovery/trending/recommendations.
2. One shared request per canonical item. No vote count and no duplicate per-user request rows.
3. The creator is automatically subscribed; other authenticated users may choose **Notify Me**.
4. Requests are visible to all authenticated users. `My Requests` is a filter only.
5. People sign in through OIDC; Keycloak is the primary tested provider. The one exception is the built-in **local administrator** (D040): a single password account that exists so a new instance can be set up and recovered without an identity provider. Do not add any other local-password account or any other way to sign in.
6. Apprise API is the external notification integration. In-app notifications remain native.
7. Libraries are authoritative for availability, not metadata providers and not acquisition tools.
8. Preserve artwork aspect ratio: posters for movie/TV/game; square for album/song/audiobook.
9. Game platform options come dynamically from IGDB and are multi-select OR semantics. `Any` is first and mutually exclusive with explicit selections.
10. External services must not block routine UI rendering. Prefer local state, background jobs, caching, and stale-but-known data.
11. Temporary library failures must never mass-mark previously available content missing.
12. First-run setup is signing in as the local administrator, then a live Get started checklist that links to Settings and asks for no values itself (D038, D040). The draft setup wizard was retired; do not reintroduce inactive drafts.

## Engineering rules

- Keep domain logic independent of HTTP, database, and provider clients.
- Put external integrations behind interfaces and use deterministic fakes in tests.
- Treat provider/library payloads as untrusted input. Validate sizes, content types, IDs, URLs, and timeouts.
- Never log access tokens, OIDC tokens, API secrets, library tokens, Apprise credentials, cookies, or Authorization headers.
- All outbound HTTP calls need context cancellation, bounded timeouts, sensible response-size limits, and explicit user-agent identification where required.
- Database migrations are append-only once merged. Never edit an applied migration to fix history; add a new migration.
- Request status and availability are separate concepts internally even when the UI collapses them into simple badges.
- State changes must emit auditable events and be idempotent.
- Reconciliation must be targeted when possible; avoid full-library/full-request N×M comparisons.
- Keep URLs/bookmarkable filters in the shared Requests page.
- UI should follow `references/mockups/final/` and `docs/UI_UX.md`, but accessibility and responsive behavior take precedence over pixel matching.
- Reference projects are for learning. Do not copy GPL/AGPL code into Requestarr unless the project license decision explicitly permits it and attribution obligations are understood.

## Required checks before completing a change

```bash
make fmt
make test
make vet
make web-check     # once dependencies are installed
```

For schema changes, add migration tests. For provider/library changes, add fixtures and failure-path tests. For authentication changes, include explicit tests for unauthorized, viewer/requester, and admin paths.

## Decision changes

If implementation reveals that an accepted decision is materially harmful, document the conflict in `docs/DECISIONS.md` and propose a replacement. Do not silently change behavior.
