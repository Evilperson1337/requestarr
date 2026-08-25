BEGIN;

-- Source configuration history is retained evidence. A configured source cannot
-- be removed or repurposed through ad-hoc SQL: doing so would sever its opaque
-- credential, status, audit, and operator-intent records. V1 therefore has no
-- deconfiguration delete path; a future retirement workflow must append an
-- audited state transition rather than remove this evidence.
CREATE FUNCTION prevent_inventory_source_configuration_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(OLD.source_id::text, 0));
    RAISE EXCEPTION 'configured inventory source configuration is retained and cannot be deleted'
        USING ERRCODE = '23514';
END $$;

CREATE TRIGGER inventory_source_configurations_retention
    BEFORE DELETE ON inventory_source_configurations
    FOR EACH ROW EXECUTE FUNCTION prevent_inventory_source_configuration_delete();

CREATE FUNCTION prevent_configured_inventory_source_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(OLD.id::text, 0));
    IF EXISTS (SELECT 1 FROM inventory_source_configurations WHERE source_id = OLD.id) THEN
        RAISE EXCEPTION 'configured inventory source is retained and cannot be deleted'
            USING ERRCODE = '23514';
    END IF;
    RETURN OLD;
END $$;

CREATE TRIGGER library_sources_configured_inventory_retention
    BEFORE DELETE ON library_sources
    FOR EACH ROW EXECUTE FUNCTION prevent_configured_inventory_source_delete();

CREATE FUNCTION prevent_configured_inventory_credential_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(OLD.owner_id::text, 0));
    IF OLD.namespace = 'inventory-source'
       AND OLD.secret_name = 'credential'
       AND EXISTS (SELECT 1 FROM inventory_source_configurations WHERE source_id = OLD.owner_id) THEN
        RAISE EXCEPTION 'configured inventory credential is retained and cannot be deleted'
            USING ERRCODE = '23514';
    END IF;
    RETURN OLD;
END $$;

CREATE TRIGGER encrypted_secrets_configured_inventory_retention
    BEFORE DELETE ON encrypted_secrets
    FOR EACH ROW EXECUTE FUNCTION prevent_configured_inventory_credential_delete();

CREATE FUNCTION prevent_inventory_source_evidence_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'inventory source audit and manual intent evidence is append-only'
        USING ERRCODE = '23514';
END $$;

CREATE TRIGGER inventory_source_configuration_audit_append_only
    BEFORE UPDATE OR DELETE ON inventory_source_configuration_audit
    FOR EACH ROW EXECUTE FUNCTION prevent_inventory_source_evidence_delete();

CREATE TRIGGER inventory_manual_sync_intents_append_only
    BEFORE DELETE ON inventory_manual_sync_intents
    FOR EACH ROW EXECUTE FUNCTION prevent_inventory_source_evidence_delete();

-- Preserve the original manual-sync disposition with the immutable audit row.
-- Audit-ID replay can then report the original result instead of inferring it
-- from the retry payload.
ALTER TABLE inventory_source_configuration_audit
    ADD COLUMN credential_identity BYTEA NOT NULL DEFAULT decode(repeat('00', 32), 'hex') CHECK (octet_length(credential_identity) = 32),
    ADD COLUMN manual_intent_replayed BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN manual_intent_coalesced BOOLEAN NOT NULL DEFAULT FALSE,
    ADD CONSTRAINT inventory_source_configuration_audit_manual_intent_outcome_check
        CHECK (NOT (manual_intent_replayed AND manual_intent_coalesced));

ALTER TABLE encrypted_secrets
    ADD COLUMN credential_identity BYTEA NOT NULL DEFAULT decode(repeat('00', 32), 'hex') CHECK (octet_length(credential_identity) = 32);

COMMIT;
