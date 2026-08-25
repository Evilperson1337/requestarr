BEGIN;

-- 000020 introduced the write-lock trigger and 000021 replaced it, but neither
-- version covered library_sources and both relied on OLD/NEW expressions that
-- are not assigned for every trigger operation. Keep those applied migrations
-- immutable and converge all installations on operation-specific access.
CREATE OR REPLACE FUNCTION lock_inventory_source_configuration_write() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
    old_source UUID;
    new_source UUID;
BEGIN
    IF TG_TABLE_NAME = 'encrypted_secrets' THEN
        IF TG_OP <> 'INSERT'
           AND OLD.namespace = 'inventory-source'
           AND OLD.secret_name = 'credential' THEN
            old_source := OLD.owner_id;
        END IF;
        IF TG_OP <> 'DELETE'
           AND NEW.namespace = 'inventory-source'
           AND NEW.secret_name = 'credential' THEN
            new_source := NEW.owner_id;
        END IF;
    ELSIF TG_TABLE_NAME = 'library_sources' THEN
        IF TG_OP <> 'INSERT' THEN
            old_source := OLD.id;
        END IF;
        IF TG_OP <> 'DELETE' THEN
            new_source := NEW.id;
        END IF;
    ELSE
        IF TG_OP <> 'INSERT' THEN
            old_source := OLD.source_id;
        END IF;
        IF TG_OP <> 'DELETE' THEN
            new_source := NEW.source_id;
        END IF;
    END IF;

    -- An UPDATE can theoretically move a row between sources. Acquire both
    -- affected locks in UUID-text order so concurrent moves cannot invert the
    -- lock order. INSERT and DELETE acquire only their assigned record's lock.
    IF old_source IS NOT NULL
       AND new_source IS NOT NULL
       AND old_source IS DISTINCT FROM new_source
       AND old_source::text > new_source::text THEN
        PERFORM pg_advisory_xact_lock(hashtextextended(new_source::text, 0));
        PERFORM pg_advisory_xact_lock(hashtextextended(old_source::text, 0));
    ELSE
        IF old_source IS NOT NULL THEN
            PERFORM pg_advisory_xact_lock(hashtextextended(old_source::text, 0));
        END IF;
        IF new_source IS NOT NULL AND new_source IS DISTINCT FROM old_source THEN
            PERFORM pg_advisory_xact_lock(hashtextextended(new_source::text, 0));
        END IF;
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER library_sources_inventory_source_write_lock
    BEFORE INSERT OR UPDATE OR DELETE ON library_sources
    FOR EACH ROW EXECUTE FUNCTION lock_inventory_source_configuration_write();

COMMIT;
