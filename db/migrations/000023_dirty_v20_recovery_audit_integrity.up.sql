BEGIN;

-- v20's dirty-state recovery path may certify the already-applied schema at
-- version 20 before replaying v21+. Restore the v19 orphan audit checks that
-- were unintentionally omitted by v20's function replacement. Applied v20–22
-- remain immutable; this append-only correction converges after normal replay.
CREATE OR REPLACE FUNCTION enforce_inventory_source_configuration_integrity() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE source UUID; revision BIGINT; audit UUID;
BEGIN
    IF TG_TABLE_NAME = 'library_sources' THEN
        source := COALESCE(NEW.id, OLD.id);
    ELSIF TG_TABLE_NAME = 'encrypted_secrets' THEN
        IF COALESCE(NEW.namespace, OLD.namespace) <> 'inventory-source'
           OR COALESCE(NEW.secret_name, OLD.secret_name) <> 'credential' THEN
            RETURN NULL;
        END IF;
        source := COALESCE(NEW.owner_id, OLD.owner_id);
    ELSIF TG_TABLE_NAME = 'inventory_source_configuration_audit_selected_libraries' THEN
        audit := COALESCE(NEW.audit_id, OLD.audit_id);
        SELECT evidence.source_id, evidence.revision INTO source, revision
          FROM inventory_source_configuration_revisions AS evidence WHERE evidence.audit_id = audit;
        IF source IS NULL THEN
            RAISE EXCEPTION 'inventory source audit selection evidence must have matching revision evidence'
                USING ERRCODE = '23514';
        END IF;
    ELSIF TG_TABLE_NAME = 'inventory_source_configuration_audit' THEN
        source := NEW.source_id;
        revision := NEW.configuration_revision;
        IF NOT EXISTS (
            SELECT 1 FROM inventory_source_configuration_revisions
             WHERE audit_id = NEW.id AND source_id = source AND revision = revision
        ) THEN
            RAISE EXCEPTION 'inventory source audit evidence must have matching revision evidence'
                USING ERRCODE = '23514';
        END IF;
    ELSE
        source := COALESCE(NEW.source_id, OLD.source_id);
        IF TG_TABLE_NAME IN ('inventory_source_configurations', 'inventory_source_configuration_revisions', 'inventory_source_configuration_revision_selected_libraries', 'inventory_source_configuration_credential_revisions') THEN
            revision := COALESCE(NEW.revision, OLD.revision);
        END IF;
    END IF;
    PERFORM assert_inventory_source_configuration_projection(source);
    PERFORM assert_inventory_source_configuration_credential_envelope(source);
    IF revision IS NOT NULL AND EXISTS (SELECT 1 FROM inventory_source_configuration_revisions WHERE source_id = source AND revision = revision) THEN
        PERFORM assert_inventory_source_configuration_revision_evidence(source, revision);
        PERFORM assert_inventory_source_configuration_credential_revision_evidence(source, revision);
    END IF;
    RETURN NULL;
END $$;

COMMIT;
