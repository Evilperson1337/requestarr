BEGIN;

-- Revision evidence is append-only. Rollback is safe only before it exists.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM inventory_source_configuration_revisions)
       OR EXISTS (SELECT 1 FROM inventory_source_configuration_revision_selected_libraries) THEN
        RAISE EXCEPTION 'cannot roll back 000018 while inventory source configuration revision evidence exists';
    END IF;
END $$;

DROP TRIGGER inventory_source_configuration_revision_selected_libraries_append_only
    ON inventory_source_configuration_revision_selected_libraries;
DROP TRIGGER inventory_source_configuration_revisions_append_only
    ON inventory_source_configuration_revisions;
DROP TABLE inventory_source_configuration_revision_selected_libraries;
DROP TABLE inventory_source_configuration_revisions;
ALTER TABLE inventory_source_configurations
    DROP CONSTRAINT inventory_source_configurations_source_revision_unique;
ALTER TABLE inventory_source_configuration_audit
    DROP CONSTRAINT inventory_source_configuration_audit_source_id_unique;
DROP FUNCTION valid_inventory_source_revision_media_types(TEXT, JSONB);

-- Restore the prior migration's strict Audiobookshelf-only invariant.
CREATE OR REPLACE FUNCTION enforce_audiobookshelf_source_configuration() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE source_kind TEXT; source_base_url TEXT; source_secret_ref TEXT;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(COALESCE(NEW.source_id, OLD.source_id)::text, 0));
    SELECT kind, base_url, secret_ref INTO source_kind, source_base_url, source_secret_ref
      FROM library_sources WHERE id = COALESCE(NEW.source_id, OLD.source_id);
    IF source_kind IS DISTINCT FROM 'audiobookshelf' OR source_base_url IS NOT NULL OR source_secret_ref IS NOT NULL THEN
        RAISE EXCEPTION 'inventory source configuration requires a non-legacy audiobookshelf source' USING ERRCODE = '23514';
    END IF;
    RETURN COALESCE(NEW, OLD);
END $$;
CREATE OR REPLACE FUNCTION enforce_configured_audiobookshelf_source() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.id::text, 0));
    IF EXISTS (SELECT 1 FROM inventory_source_configurations WHERE source_id = NEW.id)
       AND (NEW.kind IS DISTINCT FROM 'audiobookshelf' OR NEW.base_url IS NOT NULL OR NEW.secret_ref IS NOT NULL) THEN
        RAISE EXCEPTION 'configured audiobookshelf source cannot use legacy source fields or kind' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;

COMMIT;
