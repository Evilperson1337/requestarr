# Testing Strategy

## Unit
- URL recognition and provider ID extraction.
- request state transitions;
- platform Any/multi-select validation;
- subscription creation/uniqueness;
- reconciliation by exact external ID;
- fallback match confidence;
- role/capability authorization.

## Contract/fixture
Store sanitized provider/library fixtures under `testdata/` and test mapping independently of live services.

## Integration
Use PostgreSQL plus dev-compose Keycloak. Add mock HTTP servers for TMDB/IGDB/MusicBrainz/Plex/Jellyfin/Navidrome/RomM/Apprise. Live integration tests must be opt-in and never required for ordinary CI.

## End-to-end
Cover setup wizard, OIDC sign-in, create request, duplicate resolution, Notify Me, admin approval/denial, simulated inventory arrival, Available transition, notification delivery, source outage retaining last-known state.

## Performance targets (initial, validate later)
- shared Requests page backed by local DB should feel instantaneous at household-scale inventories;
- no external API call on ordinary Requests page rendering;
- resolution UI progressively shows cached data when possible;
- library sync memory use must be bounded/paginated.
