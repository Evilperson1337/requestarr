-- Lossy by necessity: the old tables are Jellyfin-only, so other Sources'
-- absence streaks and snapshot-run rows are discarded.
ALTER INDEX source_snapshot_runs_observed_idx RENAME TO jellyfin_snapshot_runs_observed_idx;
DELETE FROM source_snapshot_runs WHERE source_id <> '5efcc577-5236-4ce3-b2c0-b9d58c64a718';
ALTER TABLE source_snapshot_runs DROP COLUMN source_id;
ALTER TABLE source_snapshot_runs RENAME TO jellyfin_snapshot_runs;
ALTER TABLE source_reconciliation_settings RENAME TO jellyfin_reconciliation_settings;

CREATE TABLE jellyfin_absence_state (
    catalog_item_id UUID PRIMARY KEY REFERENCES catalog_items(id) ON DELETE CASCADE,
    first_absent_at TIMESTAMPTZ NOT NULL,
    last_absent_at TIMESTAMPTZ NOT NULL,
    absence_count BIGINT NOT NULL CHECK (absence_count >= 1)
);
INSERT INTO jellyfin_absence_state(catalog_item_id, first_absent_at, last_absent_at, absence_count)
SELECT catalog_item_id, first_absent_at, last_absent_at, absence_count
FROM source_absence_state WHERE source_id = '5efcc577-5236-4ce3-b2c0-b9d58c64a718';
DROP TABLE source_absence_state;
