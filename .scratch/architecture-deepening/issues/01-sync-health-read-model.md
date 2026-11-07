# 01: One Sync health read model and a testable snapshot-persistence seam

**What to build:** Introduce Sync health: one read model combining run outcomes from job_runs with snapshot facts from library_sources, for all four Sources. Remove the duplicated Jellyfin source ID (internal/app/app.go and internal/store/postgres/inventory.go) and the duplicated library_sources upsert (success vs failure; failure must not overwrite base_url). Delete GET /admin/sources/jellyfin/status, JellyfinSyncStatus and the JellyfinStatus TS type; the Settings page and GET /admin/status read Sync health for every Source. Split PersistSnapshot (match, presence, absence, availability) behind a narrower store interface. Add a test-only in-memory adapter and one contract suite (like inventorytest) run against both it and Postgres, covering D014, D015 and D021. Update api/openapi.yaml first. Add D026. No migration.

**Blocked by:** None (can start immediately)

**Status:** needs-review

- [ ] One definition of each Source id; a first failed sync then a success keeps the real base_url
- [ ] Settings shows sync health for Jellyfin, Plex, RomM and Navidrome from one read model
- [ ] A failed sync never produces removals (existing invariant) and is tested without a database
- [ ] The same contract suite passes against the in-memory adapter and Postgres
- [ ] Jellyfin-only route, method and TS type are gone; openapi.yaml updated
- [ ] Tests that go: per-file Jellyfin status tests. Tests that survive: existing Postgres integration tests

## Progress

Done: single `jellyfin.SourceID`; one `upsertSource` write (failure never replaces a recorded URL); `LastSnapshotTimes` plus `lastSnapshotAt` on `GET /admin/status` for all four Sources; Jellyfin-only route, `JellyfinSyncStatus`, `SourceSyncStatus` and `JellyfinStatus` removed; Settings renders and syncs every Source; openapi, INTEGRATIONS.md and D026 updated. `createRequest` was also filed under the Jellyfin status path in openapi.yaml; it is now under `/requests`.
Checks: `go test ./...`, `go vet`, `make web-check`, `make test-integration`, Playwright development-logs/status/a11y specs.

Deferred: the `PersistSnapshot` split and in-memory adapter with a shared contract suite. The in-memory adapter would reimplement matching, aliases, absence streaks, platform satisfaction and request/notification writes, so it is a second implementation of D014/D015/D021 rather than a thin fake. Decide its scope before building it.

## Follow-up (done)

The snapshot-persistence seam is now two adapters under one contract. `internal/store/snapshottest` holds `Run`, the rules as seven cases (presence makes an item available and moves its open request; an unreported item is left alone; an incomplete snapshot is refused; one absence keeps an item and a second a day later removes it; a returned item clears its streak; an empty enumeration removes nothing; one Source dropping an item does not hide it while another has it), and `Memory`, the in-memory adapter. The in-memory adapter covers canonical provider ids only; suggested matches and aliases stay the store's. `TestPostgresHoldsTheSnapshotPersistenceContract` runs the same cases against PostgreSQL, and `TestMemoryAdapterHoldsTheContract` runs them with no database. I did not split `PersistSnapshot`'s internals; the contract is at its existing interface (`sourceSyncStore`), which was already narrow.
