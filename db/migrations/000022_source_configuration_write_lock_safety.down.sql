BEGIN;

DROP TRIGGER library_sources_inventory_source_write_lock ON library_sources;

-- Restore the immediately preceding function definition for a clean rollback.
CREATE OR REPLACE FUNCTION lock_inventory_source_configuration_write() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
    source UUID;
BEGIN
    IF TG_TABLE_NAME = 'encrypted_secrets' THEN
        IF TG_OP <> 'INSERT' AND OLD.namespace = 'inventory-source' AND OLD.secret_name = 'credential' THEN
            PERFORM pg_advisory_xact_lock(hashtextextended(OLD.owner_id::text, 0));
        END IF;
        IF TG_OP <> 'DELETE' AND NEW.namespace = 'inventory-source' AND NEW.secret_name = 'credential' THEN
            PERFORM pg_advisory_xact_lock(hashtextextended(NEW.owner_id::text, 0));
        END IF;
    ELSE
        source := COALESCE(NEW.source_id, OLD.source_id, NEW.id, OLD.id);
        PERFORM pg_advisory_xact_lock(hashtextextended(source::text, 0));
    END IF;
    RETURN COALESCE(NEW, OLD);
END $$;

COMMIT;
