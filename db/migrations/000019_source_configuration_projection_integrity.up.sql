BEGIN;

-- The configuration projection is a cache of the newest immutable revision
-- evidence, not independently editable state. Deferred constraint triggers
-- let the repository replace all projection rows in one transaction while
-- preventing an ad-hoc transaction from committing a divergent projection.
CREATE FUNCTION valid_configured_source_media_types(kind TEXT, media_types JSONB) RETURNS BOOLEAN
LANGUAGE sql IMMUTABLE AS $$
    SELECT jsonb_typeof(media_types) = 'array'
       AND CASE kind
           WHEN 'audiobookshelf' THEN media_types = '["audiobook"]'::jsonb
           WHEN 'plex' THEN jsonb_array_length(media_types) BETWEEN 1 AND 4
               AND NOT EXISTS (
                    SELECT 1
                    FROM jsonb_array_elements_text(media_types) AS media_type(value)
                    WHERE media_type.value NOT IN ('movie', 'tv', 'album', 'song')
               )
               AND (SELECT count(*) FROM jsonb_array_elements_text(media_types))
                   = (SELECT count(DISTINCT value) FROM jsonb_array_elements_text(media_types) AS media_type(value))
           ELSE FALSE
       END
$$;

CREATE FUNCTION assert_inventory_source_configuration_projection(source UUID) RETURNS VOID
LANGUAGE plpgsql AS $$
DECLARE
    configured_revision BIGINT;
    latest_revision BIGINT;
BEGIN
    SELECT revision INTO configured_revision
      FROM inventory_source_configurations
     WHERE source_id = source;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT max(revision) INTO latest_revision
      FROM inventory_source_configuration_revisions
     WHERE source_id = source;
    IF latest_revision IS NULL OR configured_revision <> latest_revision THEN
        RAISE EXCEPTION 'configured inventory source projection must reference its latest revision evidence'
            USING ERRCODE = '23514';
    END IF;

    IF NOT EXISTS (
        SELECT 1
          FROM inventory_source_configurations configuration
          JOIN library_sources source_projection ON source_projection.id = configuration.source_id
          JOIN inventory_source_configuration_revisions revision_evidence
            ON revision_evidence.source_id = configuration.source_id
           AND revision_evidence.revision = configuration.revision
          JOIN inventory_source_configuration_audit audit_evidence
            ON audit_evidence.id = revision_evidence.audit_id
         WHERE configuration.source_id = source
           AND source_projection.kind = revision_evidence.source_kind
           AND source_projection.name = revision_evidence.source_name
           AND source_projection.enabled = revision_evidence.enabled
           AND source_projection.authoritative_media_types = revision_evidence.enabled_media_types
           AND configuration.endpoint_url = revision_evidence.endpoint_url
           AND valid_configured_source_media_types(source_projection.kind, source_projection.authoritative_media_types)
           AND audit_evidence.source_id = revision_evidence.source_id
           AND audit_evidence.configuration_revision = revision_evidence.revision
           AND audit_evidence.credential_identity = revision_evidence.credential_identity
           AND EXISTS (
                SELECT 1 FROM encrypted_secrets credential
                 WHERE credential.namespace = 'inventory-source'
                   AND credential.owner_id = configuration.source_id
                   AND credential.secret_name = 'credential'
                   AND credential.credential_identity = revision_evidence.credential_identity
           )
    ) THEN
        RAISE EXCEPTION 'configured inventory source projection disagrees with revision evidence'
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        (SELECT remote_library_id FROM inventory_source_selected_libraries WHERE source_id = source
         EXCEPT
         SELECT remote_library_id FROM inventory_source_configuration_revision_selected_libraries WHERE source_id = source AND revision = configured_revision)
        UNION ALL
        (SELECT remote_library_id FROM inventory_source_configuration_revision_selected_libraries WHERE source_id = source AND revision = configured_revision
         EXCEPT
         SELECT remote_library_id FROM inventory_source_selected_libraries WHERE source_id = source)
    ) THEN
        RAISE EXCEPTION 'configured inventory source selected libraries disagree with revision evidence'
            USING ERRCODE = '23514';
    END IF;
END $$;

CREATE FUNCTION assert_inventory_source_configuration_revision_evidence(source UUID, evidence_revision BIGINT) RETURNS VOID
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM inventory_source_configuration_revisions revision_evidence
          JOIN inventory_source_configuration_audit audit_evidence
            ON audit_evidence.id = revision_evidence.audit_id
         WHERE revision_evidence.source_id = source
           AND revision_evidence.revision = evidence_revision
           AND audit_evidence.source_id = revision_evidence.source_id
           AND audit_evidence.configuration_revision = revision_evidence.revision
           AND audit_evidence.credential_identity = revision_evidence.credential_identity
    ) THEN
        RAISE EXCEPTION 'inventory source revision evidence must match its audit source, revision, and credential identity'
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        (SELECT remote_library_id FROM inventory_source_configuration_revision_selected_libraries WHERE source_id = source AND revision = evidence_revision
         EXCEPT
         SELECT remote_library_id FROM inventory_source_configuration_audit_selected_libraries WHERE audit_id = (SELECT audit_id FROM inventory_source_configuration_revisions WHERE source_id = source AND revision = evidence_revision))
        UNION ALL
        (SELECT remote_library_id FROM inventory_source_configuration_audit_selected_libraries WHERE audit_id = (SELECT audit_id FROM inventory_source_configuration_revisions WHERE source_id = source AND revision = evidence_revision)
         EXCEPT
         SELECT remote_library_id FROM inventory_source_configuration_revision_selected_libraries WHERE source_id = source AND revision = evidence_revision)
    ) THEN
        RAISE EXCEPTION 'inventory source revision scope evidence must match audit selection evidence'
            USING ERRCODE = '23514';
    END IF;
END $$;

CREATE FUNCTION enforce_inventory_source_configuration_integrity() RETURNS TRIGGER
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
        IF TG_TABLE_NAME IN ('inventory_source_configurations', 'inventory_source_configuration_revisions', 'inventory_source_configuration_revision_selected_libraries') THEN
            revision := COALESCE(NEW.revision, OLD.revision);
        END IF;
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended(source::text, 0));
    PERFORM assert_inventory_source_configuration_projection(source);
    IF revision IS NOT NULL AND EXISTS (
        SELECT 1 FROM inventory_source_configuration_revisions WHERE source_id = source AND revision = revision
    ) THEN
        PERFORM assert_inventory_source_configuration_revision_evidence(source, revision);
    END IF;
    RETURN NULL;
END $$;

-- Kind is a durable part of the first configuration record. Existing sources
-- may be updated only without retyping; fresh Plex and Audiobookshelf sources
-- remain valid configuration candidates.
CREATE OR REPLACE FUNCTION enforce_configured_audiobookshelf_source() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.id::text, 0));
    IF EXISTS (SELECT 1 FROM inventory_source_configurations WHERE source_id = NEW.id)
       AND (NEW.kind NOT IN ('plex', 'audiobookshelf')
            OR NEW.base_url IS NOT NULL OR NEW.secret_ref IS NOT NULL
            OR NEW.kind IS DISTINCT FROM OLD.kind
            OR NOT valid_configured_source_media_types(NEW.kind, NEW.authoritative_media_types)) THEN
        RAISE EXCEPTION 'configured inventory source kind, media types, and legacy fields are immutable or invalid'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER library_sources_configured_audiobookshelf_invariant ON library_sources;
CREATE TRIGGER library_sources_configured_audiobookshelf_invariant
    BEFORE UPDATE OF kind, base_url, secret_ref, authoritative_media_types ON library_sources
    FOR EACH ROW EXECUTE FUNCTION enforce_configured_audiobookshelf_source();

CREATE CONSTRAINT TRIGGER inventory_source_configurations_projection_integrity
    AFTER INSERT OR UPDATE OR DELETE ON inventory_source_configurations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_inventory_source_configuration_integrity();
CREATE CONSTRAINT TRIGGER library_sources_projection_integrity
    AFTER UPDATE OF kind, name, enabled, authoritative_media_types ON library_sources
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_inventory_source_configuration_integrity();
CREATE CONSTRAINT TRIGGER inventory_source_selected_libraries_projection_integrity
    AFTER INSERT OR UPDATE OR DELETE ON inventory_source_selected_libraries
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_inventory_source_configuration_integrity();
CREATE CONSTRAINT TRIGGER encrypted_secrets_inventory_source_projection_integrity
    AFTER INSERT OR UPDATE OR DELETE ON encrypted_secrets
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_inventory_source_configuration_integrity();
CREATE CONSTRAINT TRIGGER inventory_source_configuration_revisions_integrity
    AFTER INSERT ON inventory_source_configuration_revisions
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_inventory_source_configuration_integrity();
CREATE CONSTRAINT TRIGGER inventory_source_configuration_audit_integrity
    AFTER INSERT ON inventory_source_configuration_audit
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_inventory_source_configuration_integrity();
CREATE CONSTRAINT TRIGGER inventory_source_configuration_revision_selected_libraries_integrity
    AFTER INSERT ON inventory_source_configuration_revision_selected_libraries
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_inventory_source_configuration_integrity();
CREATE CONSTRAINT TRIGGER inventory_source_configuration_audit_selected_libraries_integrity
    AFTER INSERT ON inventory_source_configuration_audit_selected_libraries
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION enforce_inventory_source_configuration_integrity();

-- Refuse installation rather than certifying pre-existing divergent state.
DO $$
DECLARE source UUID; evidence RECORD;
BEGIN
    FOR source IN SELECT configuration.source_id FROM inventory_source_configurations AS configuration LOOP
        PERFORM assert_inventory_source_configuration_projection(source);
    END LOOP;
    FOR evidence IN SELECT revision_evidence.source_id, revision_evidence.revision FROM inventory_source_configuration_revisions AS revision_evidence LOOP
        PERFORM assert_inventory_source_configuration_revision_evidence(evidence.source_id, evidence.revision);
    END LOOP;
END $$;

COMMIT;
