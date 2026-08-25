BEGIN;

-- Historical library-selection evidence must never be silently discarded.
-- A clean rollback remains possible before any new evidence is recorded.
DO $$ BEGIN
    IF EXISTS (
        SELECT 1
        FROM inventory_source_configuration_audit_selected_libraries
    ) THEN
        RAISE EXCEPTION 'cannot roll back 000016 while inventory source configuration audit library evidence exists';
    END IF;
END $$;

DROP TRIGGER inventory_source_configuration_audit_selected_libraries_append_only
    ON inventory_source_configuration_audit_selected_libraries;
DROP TABLE inventory_source_configuration_audit_selected_libraries;

COMMIT;
