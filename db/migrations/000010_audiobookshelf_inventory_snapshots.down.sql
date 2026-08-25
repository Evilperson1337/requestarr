BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM audiobook_inventory_snapshots)
       OR EXISTS (SELECT 1 FROM audiobook_source_libraries)
       OR EXISTS (SELECT 1 FROM audiobook_inventory_details)
       OR EXISTS (SELECT 1 FROM inventory_items WHERE last_snapshot_id IS NOT NULL OR tombstoned_at IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back audiobook inventory snapshots with persisted history';
    END IF;
END
$$;

DROP TABLE audiobook_inventory_details;
DROP INDEX inventory_items_source_live_snapshot_idx;
ALTER TABLE inventory_items DROP CONSTRAINT inventory_items_tombstone_snapshot_check;
ALTER TABLE inventory_items DROP COLUMN tombstoned_at;
ALTER TABLE inventory_items DROP COLUMN last_snapshot_id;
DROP TABLE audiobook_inventory_snapshots;
DROP TABLE audiobook_source_libraries;

COMMIT;
