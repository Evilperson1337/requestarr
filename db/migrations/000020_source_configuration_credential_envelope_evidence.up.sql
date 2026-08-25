BEGIN;

-- A credential identity intentionally omits randomized envelope material. Keep
-- an immutable copy of that material per configuration revision so changing an
-- envelope cannot be hidden behind an unchanged credential identity.
CREATE TABLE inventory_source_configuration_credential_revisions (
    source_id UUID NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    envelope_version SMALLINT NOT NULL CHECK (envelope_version = 1),
    key_id BYTEA NOT NULL CHECK (octet_length(key_id) = 16),
    nonce BYTEA NOT NULL CHECK (octet_length(nonce) = 12),
    ciphertext BYTEA NOT NULL CHECK (octet_length(ciphertext) BETWEEN 16 AND 65552),
    credential_identity BYTEA NOT NULL CHECK (octet_length(credential_identity) = 32),
    PRIMARY KEY (source_id, revision),
    FOREIGN KEY (source_id, revision)
        REFERENCES inventory_source_configuration_revisions(source_id, revision) ON DELETE RESTRICT
);

CREATE TRIGGER inventory_source_configuration_credential_revisions_append_only
    BEFORE UPDATE OR DELETE ON inventory_source_configuration_credential_revisions
    FOR EACH ROW EXECUTE FUNCTION prevent_inventory_source_evidence_delete();

-- Historical envelope bytes were not retained before this migration. Snapshot
-- the currently projected envelope for the current revision; all subsequent
-- revisions are written atomically by the repository.
INSERT INTO inventory_source_configuration_credential_revisions
    (source_id, revision, envelope_version, key_id, nonce, ciphertext, credential_identity)
SELECT configuration.source_id, configuration.revision, credential.envelope_version,
       credential.key_id, credential.nonce, credential.ciphertext, credential.credential_identity
  FROM inventory_source_configurations AS configuration
  JOIN encrypted_secrets AS credential
    ON credential.namespace = 'inventory-source'
   AND credential.owner_id = configuration.source_id
   AND credential.secret_name = 'credential';

CREATE OR REPLACE FUNCTION assert_inventory_source_configuration_credential_envelope(source UUID) RETURNS VOID
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM inventory_source_configurations AS configuration
          JOIN encrypted_secrets AS credential
            ON credential.namespace = 'inventory-source'
           AND credential.owner_id = configuration.source_id
           AND credential.secret_name = 'credential'
          JOIN inventory_source_configuration_credential_revisions AS evidence
            ON evidence.source_id = configuration.source_id
           AND evidence.revision = configuration.revision
         WHERE configuration.source_id = source
           AND (credential.envelope_version, credential.key_id, credential.nonce,
                credential.ciphertext, credential.credential_identity)
               IS NOT DISTINCT FROM
               (evidence.envelope_version, evidence.key_id, evidence.nonce,
                evidence.ciphertext, evidence.credential_identity)
    ) THEN
        RAISE EXCEPTION 'configured inventory source credential envelope must match immutable revision evidence'
            USING ERRCODE = '23514';
    END IF;
END $$;

-- Acquire the one source-scoped transaction lock before every mutable
-- projection write. Deferred integrity checks only inspect state: they never
-- acquire locks during COMMIT, eliminating the former lock-order inversion.
CREATE FUNCTION lock_inventory_source_configuration_write() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE source UUID;
BEGIN
    IF TG_TABLE_NAME = 'encrypted_secrets' THEN
        IF COALESCE(NEW.namespace, OLD.namespace) <> 'inventory-source'
           OR COALESCE(NEW.secret_name, OLD.secret_name) <> 'credential' THEN
            RETURN COALESCE(NEW, OLD);
        END IF;
        source := COALESCE(NEW.owner_id, OLD.owner_id);
    ELSE
        source := COALESCE(NEW.source_id, OLD.source_id, NEW.id, OLD.id);
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended(source::text, 0));
    RETURN COALESCE(NEW, OLD);
END $$;

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
        SELECT evidence.source_id, evidence.revision INTO source, revision FROM inventory_source_configuration_revisions AS evidence WHERE evidence.audit_id = audit;
    ELSIF TG_TABLE_NAME = 'inventory_source_configuration_audit' THEN
        source := NEW.source_id; revision := NEW.configuration_revision;
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
    END IF;
    RETURN NULL;
END $$;

CREATE TRIGGER inventory_source_configurations_write_lock
    BEFORE INSERT OR UPDATE OR DELETE ON inventory_source_configurations
    FOR EACH ROW EXECUTE FUNCTION lock_inventory_source_configuration_write();
CREATE TRIGGER inventory_source_selected_libraries_write_lock
    BEFORE INSERT OR UPDATE OR DELETE ON inventory_source_selected_libraries
    FOR EACH ROW EXECUTE FUNCTION lock_inventory_source_configuration_write();
CREATE TRIGGER encrypted_secrets_inventory_source_write_lock
    BEFORE INSERT OR UPDATE OR DELETE ON encrypted_secrets
    FOR EACH ROW EXECUTE FUNCTION lock_inventory_source_configuration_write();

CREATE CONSTRAINT TRIGGER inventory_source_configuration_credential_revisions_integrity
    AFTER INSERT ON inventory_source_configuration_credential_revisions
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_inventory_source_configuration_integrity();

DO $$
DECLARE source UUID;
BEGIN
    FOR source IN SELECT source_id FROM inventory_source_configurations LOOP
        PERFORM assert_inventory_source_configuration_credential_envelope(source);
    END LOOP;
END $$;

COMMIT;
