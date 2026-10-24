# PostgreSQL integration baseline

Recorded 2026-10-21 with `make test-integration` (PostgreSQL 17 via `compose.dev.yaml`, database `requestarr_test`).

**Result: FAIL** (2 failures; all other tests in `internal/store/postgres` passed).

| Test | Failure |
| --- | --- |
| `TestJellyfinSnapshotIsPositiveIdempotentEvidence` | `scan Jellyfin availability candidate: can't scan into dest[5] (col: absence_count): cannot scan NULL into *int64` (`postgres_integration_test.go:175`) |
| `TestJellyfinRequiresTwoCompleteAbsencesBeforeMarkingMissing` | same scan error (`postgres_integration_test.go:220`) |

Both point at the Jellyfin availability candidate query returning a NULL `absence_count` that the Go scan target cannot accept. This contradicts the 2026-10-20 note in `docs/IMPLEMENTATION_PLAN.md` that the full integration suite passed; treat that claim as stale until these are fixed.

**Update 2026-10-22 (ticket 07):** the `absence_count` NULL scan is fixed (`COALESCE` in the candidate query) and `TestJellyfinRequiresTwoCompleteAbsencesBeforeMarkingMissing` now sends non-empty snapshots, since empty snapshots deliberately never count as absence. The suite passes.

**Update 2026-10-24 (ticket 28):** `make test-integration` passes. A later `TestPlexMusicMakesAlbumAndSongRequestsAvailableThroughMusicBrainzAliases` failure was test pollution, not a product bug: its cleanup ignored a foreign-key error, leaving open music requests that crowded the test's 50-item work list in the persistent test database. The cleanup now deletes requests first and reports failures.
