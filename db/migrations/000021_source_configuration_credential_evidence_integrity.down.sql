BEGIN;

-- Never expose 000020's destructive rollback while immutable envelope
-- evidence exists.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM inventory_source_configuration_credential_revisions) THEN
        RAISE EXCEPTION 'credential envelope evidence exists; refusing rollback'
            USING ERRCODE = '55000';
    END IF;
END $$;

DROP TRIGGER inventory_source_configuration_credential_revisions_identity_integrity ON inventory_source_configuration_credential_revisions;
DROP FUNCTION enforce_inventory_source_configuration_credential_evidence_integrity();
DROP FUNCTION assert_inventory_source_configuration_credential_revision_evidence(UUID, BIGINT);
DROP TRIGGER encrypted_secrets_configured_inventory_credential_identity ON encrypted_secrets;
DROP FUNCTION prevent_configured_inventory_source_credential_reclassification();

COMMIT;
