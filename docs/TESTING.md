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

### Running the PostgreSQL integration suite

```bash
make test-integration
```

This starts the dev-compose `postgres` service, creates a dedicated `requestarr_test` database (never the application's `requestarr` database), and runs `go test -tags integration ./internal/store/postgres/...`. Without the `integration` tag, or without `REQUESTARR_TEST_DATABASE_URL`, these tests are skipped.

- If host port 5432 is taken, use `make test-integration REQUESTARR_POSTGRES_PORT=5433`.
- To use your own disposable database, set `REQUESTARR_TEST_DATABASE_URL`; the target then skips compose and runs against it.
- `scripts/check.sh` runs the suite when `REQUESTARR_TEST_DATABASE_URL` is set, and skips it otherwise.

## End-to-end
Cover setup wizard, OIDC sign-in, create request, duplicate resolution, Notify Me, admin approval/denial, simulated inventory arrival, Available transition, notification delivery, source outage retaining last-known state.

## Performance targets (initial, validate later)
- shared Requests page backed by local DB should feel instantaneous at household-scale inventories;
- no external API call on ordinary Requests page rendering;
- resolution UI progressively shows cached data when possible;
- library sync memory use must be bounded/paginated.
# Focused Requests browser tests

The Requests page has a Playwright suite with a deterministic mocked API. It
does not require PostgreSQL, a live identity provider, or library credentials.
Playwright is a development-only dependency; no browser framework is shipped
with the app. Broader page smoke coverage and CI integration remain V1 ticket #24.

```sh
make web-install
cd web && npx playwright install chromium && cd ..
make web-check
make web-test
```

On a minimal Linux host, Playwright may also require its documented browser OS
dependencies. The suite starts its own loopback-only Vite server on port 4173;
keep that port free. It never reuses an unrelated running server.

Tests cover bookmarked controls/reloads, URL paging and Back/Forward, cursor
expiry recovery, role-based actions, CSRF, last-subscriber confirmation, pending
action guards, error retention, stale response cancellation, keyboard controls,
and phone-width artwork/overflow. Test source/config are included in web-check.
Screenshots/traces are saved under `web/test-results/` on failure and ignored by
Git; a phone-layout screenshot is also produced on a successful layout test.
For a focused rerun: `cd web && npm test -- --grep 'phone layout'`.
