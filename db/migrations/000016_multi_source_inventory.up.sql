-- Generalize Jellyfin-only reconciliation state to every library Source (D015).
-- Absence is tracked per (Source, catalog item): an item becomes missing only
-- after every covering Source has completed syncs without it.
CREATE TABLE source_absence_state (
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE CASCADE,
    catalog_item_id UUID NOT NULL REFERENCES catalog_items(id) ON DELETE CASCADE,
    first_absent_at TIMESTAMPTZ NOT NULL,
    last_absent_at TIMESTAMPTZ NOT NULL,
    absence_count BIGINT NOT NULL CHECK (absence_count >= 1),
    PRIMARY KEY (source_id, catalog_item_id)
);

-- Existing Jellyfin streaks carry over under the Jellyfin Source's fixed ID.
INSERT INTO source_absence_state(source_id, catalog_item_id, first_absent_at, last_absent_at, absence_count)
SELECT '5efcc577-5236-4ce3-b2c0-b9d58c64a718'::uuid, a.catalog_item_id, a.first_absent_at, a.last_absent_at, a.absence_count
FROM jellyfin_absence_state a
WHERE EXISTS (SELECT 1 FROM library_sources WHERE id = '5efcc577-5236-4ce3-b2c0-b9d58c64a718');
DROP TABLE jellyfin_absence_state;

ALTER TABLE jellyfin_reconciliation_settings RENAME TO source_reconciliation_settings;

ALTER TABLE jellyfin_snapshot_runs RENAME TO source_snapshot_runs;
ALTER TABLE source_snapshot_runs ADD COLUMN source_id UUID;
UPDATE source_snapshot_runs SET source_id = '5efcc577-5236-4ce3-b2c0-b9d58c64a718';
ALTER TABLE source_snapshot_runs ALTER COLUMN source_id SET NOT NULL;
ALTER INDEX jellyfin_snapshot_runs_observed_idx RENAME TO source_snapshot_runs_observed_idx;
