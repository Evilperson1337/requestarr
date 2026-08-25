BEGIN;

-- Refuse rollback while 000014-owned data exists. Dropping it would discard
-- encrypted credential ownership and pending operator intent.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM inventory_source_configurations)
       OR EXISTS (SELECT 1 FROM inventory_source_configuration_audit)
       OR EXISTS (SELECT 1 FROM inventory_manual_sync_intents) THEN
        RAISE EXCEPTION 'cannot roll back 000014 while source configuration data exists';
    END IF;
    IF EXISTS (SELECT 1 FROM library_sources WHERE base_url IS NULL) THEN
        RAISE EXCEPTION 'cannot roll back 000014 while source rows require nullable base_url';
    END IF;
END $$;

DROP TABLE inventory_manual_sync_intents;
DROP TABLE inventory_source_configuration_audit;
DROP TABLE inventory_source_selected_libraries;
DROP TRIGGER library_sources_configured_audiobookshelf_invariant ON library_sources;
DROP FUNCTION enforce_configured_audiobookshelf_source();
DROP TRIGGER inventory_source_configurations_invariant ON inventory_source_configurations;
DROP FUNCTION enforce_audiobookshelf_source_configuration();
DROP TABLE inventory_source_configurations;
ALTER TABLE library_sources ALTER COLUMN base_url SET NOT NULL;

COMMIT;
