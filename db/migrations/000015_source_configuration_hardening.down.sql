BEGIN;

-- 000015 only permits a rollback before any source configuration state has
-- been recorded. Dropping its columns or retention triggers after that point
-- would weaken evidence retention and discard credential identity data.
DO $$ BEGIN
    IF EXISTS (
        SELECT 1
        FROM inventory_source_configurations
    ) THEN
        RAISE EXCEPTION 'cannot roll back 000015 while inventory source configurations exist';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM inventory_source_configuration_audit
    ) THEN
        RAISE EXCEPTION 'cannot roll back 000015 while inventory source configuration audit evidence exists';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM inventory_manual_sync_intents
    ) THEN
        RAISE EXCEPTION 'cannot roll back 000015 while inventory manual sync intents exist';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM encrypted_secrets
        WHERE namespace = 'inventory-source'
          AND secret_name = 'credential'
    ) THEN
        RAISE EXCEPTION 'cannot roll back 000015 while inventory source credentials exist';
    END IF;
END $$;

ALTER TABLE inventory_source_configuration_audit
    DROP CONSTRAINT inventory_source_configuration_audit_manual_intent_outcome_check,
    DROP COLUMN manual_intent_coalesced,
    DROP COLUMN manual_intent_replayed,
    DROP COLUMN credential_identity;

ALTER TABLE encrypted_secrets
    DROP COLUMN credential_identity;

DROP TRIGGER inventory_manual_sync_intents_append_only ON inventory_manual_sync_intents;
DROP TRIGGER inventory_source_configuration_audit_append_only ON inventory_source_configuration_audit;
DROP FUNCTION prevent_inventory_source_evidence_delete();
DROP TRIGGER encrypted_secrets_configured_inventory_retention ON encrypted_secrets;
DROP FUNCTION prevent_configured_inventory_credential_delete();
DROP TRIGGER library_sources_configured_inventory_retention ON library_sources;
DROP FUNCTION prevent_configured_inventory_source_delete();
DROP TRIGGER inventory_source_configurations_retention ON inventory_source_configurations;
DROP FUNCTION prevent_inventory_source_configuration_delete();

COMMIT;
