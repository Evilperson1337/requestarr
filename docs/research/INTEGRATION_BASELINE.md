# PostgreSQL integration baseline

Recorded 2026-10-21 with `make test-integration` (PostgreSQL 17 via `compose.dev.yaml`, database `requestarr_test`).

**Result: FAIL** (2 failures; all other tests in `internal/store/postgres` passed).

| Test | Failure |
| --- | --- |
| `TestJellyfinSnapshotIsPositiveIdempotentEvidence` | `scan Jellyfin availability candidate: can't scan into dest[5] (col: absence_count): cannot scan NULL into *int64` (`postgres_integration_test.go:175`) |
| `TestJellyfinRequiresTwoCompleteAbsencesBeforeMarkingMissing` | same scan error (`postgres_integration_test.go:220`) |

Both point at the Jellyfin availability candidate query returning a NULL `absence_count` that the Go scan target cannot accept. This contradicts the 2026-10-20 note in `docs/IMPLEMENTATION_PLAN.md` that the full integration suite passed; treat that claim as stale until these are fixed.
