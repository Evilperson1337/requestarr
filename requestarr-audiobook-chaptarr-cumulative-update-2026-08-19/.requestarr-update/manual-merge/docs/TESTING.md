# Testing Strategy

All Go checks use Go 1.26 or newer. Go 1.26 is the unified minimum and current toolchain across local development, CI, containers, and releases.

## Unit
- URL recognition and provider ID extraction.
- request state transitions;
- platform Any/multi-select validation;
- Audible URL/ASIN/region parsing and audiobook normalization;
- audiobook exact-ASIN and narrator-aware fallback reconciliation;
- subscription creation/uniqueness;
- reconciliation by exact external ID;
- fallback match confidence;
- role/capability authorization.

## Contract/fixture
Store sanitized provider/library fixtures under `testdata/` and test mapping independently of live services.

## Integration
Use PostgreSQL plus dev-compose Keycloak. Add mock HTTP servers for TMDB/IGDB/MusicBrainz/Audnexus/Plex/Jellyfin/Navidrome/RomM/Audiobookshelf/Apprise. Live integration tests must be opt-in and never required for ordinary CI.

PostgreSQL persistence tests use the `integration` build tag and require an explicit `REQUESTARR_TEST_DATABASE_URL`. They never read `REQUESTARR_DATABASE_URL`. Each test creates a cryptographically random `requestarr_test_` schema, pins `search_path` to that schema, and drops only that verified generated schema during cleanup.

From the host with the development PostgreSQL service running:

```bash
REQUESTARR_TEST_DATABASE_URL='postgres://requestarr:requestarr@localhost:5432/requestarr?sslmode=disable' \
  go test -tags=integration ./internal/store/...
```

From inside the devcontainer, use the service hostname `postgres` instead of `localhost`.

Encrypted-secret coverage includes canonical key parsing and redaction (including setup-token/key equality rejection), AES-GCM tamper and associated-data rejection, nonce uniqueness, bounded fields, wrong/missing-key startup, concurrent first-key registration, matching-key restart, cancellation, repeated/concurrent atomic upsert, update timestamps, caller-owned transaction rollback, arbitrary wrong-key envelope rejection, and malformed-row database constraints. Configuration and application tests assert stable generic errors/logs do not contain encoded key material; repository tests verify plaintext is recoverable only through decryption. Tagged tests compile without a configured database; real PostgreSQL execution remains conditional on `REQUESTARR_TEST_DATABASE_URL`.

## End-to-end
Cover setup wizard, OIDC sign-in, create request, duplicate resolution, Notify Me, admin approval/denial, simulated inventory arrival, audiobook availability through Audiobookshelf, Available transition, notification delivery, source outage retaining last-known state.

## Performance targets (initial, validate later)
- shared Requests page backed by local DB should feel instantaneous at household-scale inventories;
- no external API call on ordinary Requests page rendering;
- resolution UI progressively shows cached data when possible;
- library sync memory use must be bounded/paginated.

## Reconciliation confidence tests

Add table-driven coverage for match method/confidence rules. Exact ASIN+region and other stable external-ID matches may auto-fulfill. High-confidence title+author+narrator matches may auto-fulfill only when unambiguous. Title+author-only, narrator-conflicting, dramatized-vs-standard, and otherwise ambiguous candidates must remain `possible` and must not transition the request to `available`. Persisted match evidence must remain bounded and secret-free.
