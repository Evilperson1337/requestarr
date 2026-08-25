BEGIN;

-- Configuration revisions are immutable evidence, separate from the mutable
-- current projection. The composite key lets scope evidence prove precisely
-- which source revision selected each remote library.
ALTER TABLE inventory_source_configurations
    ADD CONSTRAINT inventory_source_configurations_source_revision_unique
    UNIQUE (source_id, revision);

ALTER TABLE inventory_source_configuration_audit
    ADD CONSTRAINT inventory_source_configuration_audit_source_id_unique
    UNIQUE (source_id, id);

CREATE FUNCTION valid_inventory_source_revision_media_types(kind TEXT, media_types JSONB) RETURNS BOOLEAN
LANGUAGE sql IMMUTABLE AS $$
    SELECT jsonb_typeof(media_types) = 'array'
       AND NOT EXISTS (
            SELECT 1 FROM jsonb_array_elements_text(media_types) AS media_type(value)
            WHERE (kind = 'plex' AND media_type.value NOT IN ('movie', 'tv', 'album', 'song'))
               OR (kind = 'audiobookshelf' AND media_type.value <> 'audiobook')
       )
$$;

CREATE TABLE inventory_source_configuration_revisions (
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE RESTRICT,
    revision BIGINT NOT NULL CHECK (revision > 0),
    source_kind TEXT NOT NULL CHECK (source_kind IN ('plex', 'audiobookshelf')),
    source_name TEXT NOT NULL CHECK (octet_length(source_name) BETWEEN 1 AND 512),
    endpoint_url TEXT NOT NULL CHECK (octet_length(endpoint_url) BETWEEN 1 AND 2048),
    enabled BOOLEAN NOT NULL,
    enabled_media_types JSONB NOT NULL CHECK (
        jsonb_typeof(enabled_media_types) = 'array'
        AND jsonb_array_length(enabled_media_types) BETWEEN 1 AND 6
        AND valid_inventory_source_revision_media_types(source_kind, enabled_media_types)
    ),
    credential_identity BYTEA NOT NULL CHECK (octet_length(credential_identity) = 32),
    audit_id UUID NOT NULL UNIQUE,
    recorded_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (source_id, revision),
    FOREIGN KEY (source_id, audit_id)
        REFERENCES inventory_source_configuration_audit(source_id, id) ON DELETE RESTRICT
);

CREATE TABLE inventory_source_configuration_revision_selected_libraries (
    source_id UUID NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    remote_library_id TEXT NOT NULL CHECK (octet_length(remote_library_id) BETWEEN 1 AND 256),
    PRIMARY KEY (source_id, revision, remote_library_id),
    FOREIGN KEY (source_id, revision)
        REFERENCES inventory_source_configuration_revisions(source_id, revision) ON DELETE RESTRICT
);

CREATE TRIGGER inventory_source_configuration_revisions_append_only
    BEFORE UPDATE OR DELETE ON inventory_source_configuration_revisions
    FOR EACH ROW EXECUTE FUNCTION prevent_inventory_source_evidence_delete();
CREATE TRIGGER inventory_source_configuration_revision_selected_libraries_append_only
    BEFORE UPDATE OR DELETE ON inventory_source_configuration_revision_selected_libraries
    FOR EACH ROW EXECUTE FUNCTION prevent_inventory_source_evidence_delete();

-- 000014 originally constrained the new configuration projection to
-- Audiobookshelf. The same non-legacy invariant now supports the explicitly
-- configured Plex kind without admitting arbitrary source kinds.
CREATE OR REPLACE FUNCTION enforce_audiobookshelf_source_configuration() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    source_kind TEXT;
    source_base_url TEXT;
    source_secret_ref TEXT;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(COALESCE(NEW.source_id, OLD.source_id)::text, 0));
    SELECT kind, base_url, secret_ref INTO source_kind, source_base_url, source_secret_ref
      FROM library_sources WHERE id = COALESCE(NEW.source_id, OLD.source_id);
    IF source_kind NOT IN ('plex', 'audiobookshelf')
       OR source_base_url IS NOT NULL OR source_secret_ref IS NOT NULL THEN
        RAISE EXCEPTION 'inventory source configuration requires a non-legacy Plex or Audiobookshelf source'
            USING ERRCODE = '23514';
    END IF;
    RETURN COALESCE(NEW, OLD);
END $$;

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

COMMIT;
