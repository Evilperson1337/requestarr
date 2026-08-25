BEGIN;

-- Downgrading would restore the reviewed 000018 Plex-incompatible invariant.
-- Never leave retained Plex projection or evidence behind under that schema.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM library_sources WHERE kind = 'plex' AND id IN (SELECT source_id FROM inventory_source_configurations))
       OR EXISTS (SELECT 1 FROM inventory_source_configuration_revisions WHERE source_kind = 'plex') THEN
        RAISE EXCEPTION 'cannot roll back 000019 while Plex source configuration projection or revision evidence exists';
    END IF;
END $$;

DROP TRIGGER inventory_source_configuration_audit_selected_libraries_integrity ON inventory_source_configuration_audit_selected_libraries;
DROP TRIGGER inventory_source_configuration_revision_selected_libraries_integrity ON inventory_source_configuration_revision_selected_libraries;
DROP TRIGGER inventory_source_configuration_audit_integrity ON inventory_source_configuration_audit;
DROP TRIGGER inventory_source_configuration_revisions_integrity ON inventory_source_configuration_revisions;
DROP TRIGGER encrypted_secrets_inventory_source_projection_integrity ON encrypted_secrets;
DROP TRIGGER inventory_source_selected_libraries_projection_integrity ON inventory_source_selected_libraries;
DROP TRIGGER library_sources_projection_integrity ON library_sources;
DROP TRIGGER inventory_source_configurations_projection_integrity ON inventory_source_configurations;
DROP FUNCTION enforce_inventory_source_configuration_integrity();
DROP FUNCTION assert_inventory_source_configuration_revision_evidence(UUID, BIGINT);
DROP FUNCTION assert_inventory_source_configuration_projection(UUID);
DROP FUNCTION valid_configured_source_media_types(TEXT, JSONB);

CREATE OR REPLACE FUNCTION enforce_configured_audiobookshelf_source() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.id::text, 0));
    IF EXISTS (SELECT 1 FROM inventory_source_configurations WHERE source_id = NEW.id)
       AND (NEW.kind NOT IN ('plex', 'audiobookshelf')
            OR NEW.base_url IS NOT NULL OR NEW.secret_ref IS NOT NULL) THEN
        RAISE EXCEPTION 'configured inventory source cannot use legacy fields or an unsupported kind'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER library_sources_configured_audiobookshelf_invariant ON library_sources;
CREATE TRIGGER library_sources_configured_audiobookshelf_invariant
    BEFORE UPDATE OF kind, base_url, secret_ref ON library_sources
    FOR EACH ROW EXECUTE FUNCTION enforce_configured_audiobookshelf_source();

COMMIT;
