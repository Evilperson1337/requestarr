BEGIN;

-- New source configuration is intentionally separate from the legacy columns
-- on library_sources. New writers must use encrypted_secrets, never secret_ref.
ALTER TABLE library_sources ALTER COLUMN base_url DROP NOT NULL;

CREATE TABLE inventory_source_configurations (
    source_id UUID PRIMARY KEY REFERENCES library_sources(id) ON DELETE RESTRICT,
    endpoint_url TEXT NOT NULL CHECK (octet_length(endpoint_url) BETWEEN 1 AND 2048),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_at TIMESTAMPTZ NOT NULL
);

-- A configured Audiobookshelf source owns its endpoint and credential only in
-- the 000014 tables.  The trigger closes the cross-table invariant that a
-- CHECK constraint cannot express, and prevents legacy source IDs from being
-- silently repurposed by direct SQL as well as repository writers.
CREATE FUNCTION enforce_audiobookshelf_source_configuration() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    source_kind TEXT;
    source_base_url TEXT;
    source_secret_ref TEXT;
BEGIN
    -- The paired source trigger uses the same source-scoped lock. This also
    -- protects the absent-configuration path that a row lock cannot cover.
    PERFORM pg_advisory_xact_lock(hashtextextended(COALESCE(NEW.source_id, OLD.source_id)::text, 0));
    SELECT kind, base_url, secret_ref
      INTO source_kind, source_base_url, source_secret_ref
      FROM library_sources
     WHERE id = COALESCE(NEW.source_id, OLD.source_id);

    IF source_kind IS DISTINCT FROM 'audiobookshelf'
       OR source_base_url IS NOT NULL
       OR source_secret_ref IS NOT NULL THEN
        RAISE EXCEPTION 'inventory source configuration requires a non-legacy audiobookshelf source'
            USING ERRCODE = '23514';
    END IF;
    RETURN COALESCE(NEW, OLD);
END $$;

CREATE TRIGGER inventory_source_configurations_invariant
    BEFORE INSERT OR UPDATE ON inventory_source_configurations
    FOR EACH ROW EXECUTE FUNCTION enforce_audiobookshelf_source_configuration();

CREATE FUNCTION enforce_configured_audiobookshelf_source() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.id::text, 0));
    IF EXISTS (SELECT 1 FROM inventory_source_configurations WHERE source_id = NEW.id)
       AND (NEW.kind IS DISTINCT FROM 'audiobookshelf'
            OR NEW.base_url IS NOT NULL
            OR NEW.secret_ref IS NOT NULL) THEN
        RAISE EXCEPTION 'configured audiobookshelf source cannot use legacy source fields or kind'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER library_sources_configured_audiobookshelf_invariant
    BEFORE UPDATE OF kind, base_url, secret_ref ON library_sources
    FOR EACH ROW EXECUTE FUNCTION enforce_configured_audiobookshelf_source();

CREATE TABLE inventory_source_selected_libraries (
    source_id UUID NOT NULL REFERENCES inventory_source_configurations(source_id) ON DELETE CASCADE,
    remote_library_id TEXT NOT NULL CHECK (octet_length(remote_library_id) BETWEEN 1 AND 256),
    PRIMARY KEY (source_id, remote_library_id)
);

CREATE TABLE inventory_source_configuration_audit (
    id UUID PRIMARY KEY,
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE RESTRICT,
    action TEXT NOT NULL CHECK (action IN ('configured', 'updated')),
    configuration_revision BIGINT NOT NULL CHECK (configuration_revision > 0),
    command_fingerprint BYTEA NOT NULL CHECK (octet_length(command_fingerprint) = 32),
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    CHECK (actor_id IS NULL OR actor_id <> source_id)
);
CREATE INDEX inventory_source_configuration_audit_source_occurred_idx
    ON inventory_source_configuration_audit(source_id, occurred_at DESC);

-- This is a durable requested-work record, not a job lease. A later executor
-- may atomically claim it without coupling source configuration to app wiring.
CREATE TABLE inventory_manual_sync_intents (
    id UUID PRIMARY KEY,
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE RESTRICT,
    requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
    requested_at TIMESTAMPTZ NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'consumed'))
);
CREATE UNIQUE INDEX inventory_manual_sync_intents_one_pending_idx
    ON inventory_manual_sync_intents(source_id) WHERE state = 'pending';
CREATE INDEX inventory_manual_sync_intents_pending_requested_idx
    ON inventory_manual_sync_intents(requested_at, source_id) WHERE state = 'pending';

COMMIT;
