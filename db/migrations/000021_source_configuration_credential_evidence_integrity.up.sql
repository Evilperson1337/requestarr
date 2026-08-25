BEGIN;

-- 000020 attempted to create this function even though the function already
-- exists on 000019 databases. The preceding migration is intentionally left
-- immutable; this replacement makes migrated databases converge safely.
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

CREATE OR REPLACE FUNCTION prevent_configured_inventory_source_credential_reclassification() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.namespace = 'inventory-source'
       AND OLD.secret_name = 'credential'
       AND EXISTS (SELECT 1 FROM inventory_source_configurations WHERE source_id = OLD.owner_id)
       AND (NEW.namespace, NEW.secret_name, NEW.owner_id)
           IS DISTINCT FROM (OLD.namespace, OLD.secret_name, OLD.owner_id) THEN
        RAISE EXCEPTION 'configured inventory source credential namespace, name, and owner are immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER encrypted_secrets_configured_inventory_credential_identity
    BEFORE UPDATE ON encrypted_secrets
    FOR EACH ROW EXECUTE FUNCTION prevent_configured_inventory_source_credential_reclassification();

CREATE OR REPLACE FUNCTION assert_inventory_source_configuration_credential_revision_evidence(source UUID, evidence_revision BIGINT) RETURNS VOID
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM inventory_source_configuration_credential_revisions AS credential_evidence
          JOIN inventory_source_configuration_revisions AS revision_evidence
            ON revision_evidence.source_id = credential_evidence.source_id
           AND revision_evidence.revision = credential_evidence.revision
          JOIN inventory_source_configuration_audit AS audit_evidence
            ON audit_evidence.id = revision_evidence.audit_id
         WHERE credential_evidence.source_id = source
           AND credential_evidence.revision = evidence_revision
           AND credential_evidence.credential_identity = revision_evidence.credential_identity
           AND credential_evidence.credential_identity = audit_evidence.credential_identity
           AND audit_evidence.source_id = revision_evidence.source_id
           AND audit_evidence.configuration_revision = revision_evidence.revision
    ) THEN
        RAISE EXCEPTION 'credential envelope evidence must match revision and audit credential identity'
            USING ERRCODE = '23514';
    END IF;
END $$;

CREATE OR REPLACE FUNCTION enforce_inventory_source_configuration_credential_evidence_integrity() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE source UUID; revision BIGINT;
BEGIN
    source := COALESCE(NEW.source_id, OLD.source_id);
    revision := COALESCE(NEW.revision, OLD.revision);
    PERFORM assert_inventory_source_configuration_credential_revision_evidence(source, revision);
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER inventory_source_configuration_credential_revisions_identity_integrity
    AFTER INSERT ON inventory_source_configuration_credential_revisions
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_inventory_source_configuration_credential_evidence_integrity();

-- Reject legacy direct inserts that 000020 did not bind to their revision and
-- audit identity before certifying this migration.
DO $$
DECLARE evidence RECORD;
BEGIN
    FOR evidence IN SELECT source_id, revision FROM inventory_source_configuration_credential_revisions LOOP
        PERFORM assert_inventory_source_configuration_credential_revision_evidence(evidence.source_id, evidence.revision);
    END LOOP;
END $$;

COMMIT;
