BEGIN;

-- An accepted observation is immutable evidence for one exact, configured
-- source revision. The current desired-state projection below is deliberately
-- separate from this evidence and never touches legacy inventory_items.
CREATE TABLE inventory_accepted_observations (
    id UUID PRIMARY KEY,
    source_id UUID NOT NULL,
    configuration_revision BIGINT NOT NULL CHECK (configuration_revision > 0),
    command_fingerprint BYTEA NOT NULL CHECK (octet_length(command_fingerprint) = 32),
    accepted_at TIMESTAMPTZ NOT NULL,
    UNIQUE (id, source_id, configuration_revision),
    FOREIGN KEY (source_id, configuration_revision)
        REFERENCES inventory_source_configuration_revisions(source_id, revision) ON DELETE RESTRICT
);

CREATE TABLE inventory_accepted_observation_coverage (
    observation_id UUID NOT NULL REFERENCES inventory_accepted_observations(id) ON DELETE RESTRICT,
    remote_library_id TEXT NOT NULL CHECK (octet_length(remote_library_id) BETWEEN 1 AND 256),
    media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'tv', 'album', 'song', 'game', 'audiobook')),
    coverage_status TEXT NOT NULL CHECK (coverage_status IN ('verified_complete', 'inapplicable')),
    PRIMARY KEY (observation_id, remote_library_id, media_type)
);

CREATE TABLE inventory_accepted_observation_items (
    observation_id UUID NOT NULL REFERENCES inventory_accepted_observations(id) ON DELETE RESTRICT,
    remote_library_id TEXT NOT NULL CHECK (octet_length(remote_library_id) BETWEEN 1 AND 256),
    source_item_id TEXT NOT NULL CHECK (octet_length(source_item_id) BETWEEN 1 AND 256),
    media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'tv', 'album', 'song', 'game', 'audiobook')),
    title TEXT NOT NULL CHECK (octet_length(title) BETWEEN 1 AND 512),
    external_url TEXT NOT NULL DEFAULT '' CHECK (octet_length(external_url) <= 2048),
    item_revision TEXT NOT NULL CHECK (octet_length(item_revision) BETWEEN 1 AND 256),
    PRIMARY KEY (observation_id, remote_library_id, source_item_id)
);

CREATE TABLE inventory_desired_source_revision_items (
    source_id UUID NOT NULL,
    configuration_revision BIGINT NOT NULL CHECK (configuration_revision > 0),
    remote_library_id TEXT NOT NULL CHECK (octet_length(remote_library_id) BETWEEN 1 AND 256),
    source_item_id TEXT NOT NULL CHECK (octet_length(source_item_id) BETWEEN 1 AND 256),
    media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'tv', 'album', 'song', 'game', 'audiobook')),
    title TEXT NOT NULL CHECK (octet_length(title) BETWEEN 1 AND 512),
    external_url TEXT NOT NULL DEFAULT '' CHECK (octet_length(external_url) <= 2048),
    item_revision TEXT NOT NULL CHECK (octet_length(item_revision) BETWEEN 1 AND 256),
    last_observation_id UUID NOT NULL,
    PRIMARY KEY (source_id, configuration_revision, remote_library_id, source_item_id),
    FOREIGN KEY (source_id, configuration_revision)
        REFERENCES inventory_source_configuration_revisions(source_id, revision) ON DELETE RESTRICT,
    FOREIGN KEY (last_observation_id, source_id, configuration_revision)
        REFERENCES inventory_accepted_observations(id, source_id, configuration_revision) ON DELETE RESTRICT
);

CREATE TABLE inventory_accepted_observation_events (
    observation_id UUID NOT NULL REFERENCES inventory_accepted_observations(id) ON DELETE RESTRICT,
    ordinal INTEGER NOT NULL CHECK (ordinal > 0),
    change_kind TEXT NOT NULL CHECK (change_kind IN ('added', 'updated', 'removed')),
    remote_library_id TEXT NOT NULL CHECK (octet_length(remote_library_id) BETWEEN 1 AND 256),
    source_item_id TEXT NOT NULL CHECK (octet_length(source_item_id) BETWEEN 1 AND 256),
    media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'tv', 'album', 'song', 'game', 'audiobook')),
    PRIMARY KEY (observation_id, ordinal),
    UNIQUE (observation_id, remote_library_id, source_item_id)
);

CREATE FUNCTION validate_accepted_observation_scope() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    observation_source UUID;
    observation_revision BIGINT;
    expected_pairs INTEGER;
    actual_pairs INTEGER;
BEGIN
    SELECT source_id, configuration_revision
      INTO observation_source, observation_revision
      FROM inventory_accepted_observations WHERE id = NEW.observation_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'accepted observation must exist' USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM inventory_source_configuration_revision_selected_libraries library
         WHERE library.source_id = observation_source AND library.revision = observation_revision
           AND library.remote_library_id = NEW.remote_library_id
    ) OR NOT EXISTS (
        SELECT 1 FROM inventory_source_configuration_revisions revision
         WHERE revision.source_id = observation_source AND revision.revision = observation_revision
           AND revision.enabled_media_types ? NEW.media_type
    ) THEN
        RAISE EXCEPTION 'accepted observation evidence is outside its immutable configured scope' USING ERRCODE = '23514';
    END IF;
    IF TG_TABLE_NAME = 'inventory_accepted_observation_items' AND NOT EXISTS (
        SELECT 1 FROM inventory_accepted_observation_coverage coverage
         WHERE coverage.observation_id = NEW.observation_id
           AND coverage.remote_library_id = NEW.remote_library_id
           AND coverage.media_type = NEW.media_type
           AND coverage.coverage_status = 'verified_complete'
    ) THEN
        RAISE EXCEPTION 'accepted observation item lacks verified coverage provenance' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;

CREATE FUNCTION validate_accepted_observation_coverage_complete() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    observation_source UUID;
    observation_revision BIGINT;
    expected_pairs INTEGER;
    actual_pairs INTEGER;
BEGIN
    SELECT source_id, configuration_revision
      INTO observation_source, observation_revision
      FROM inventory_accepted_observations WHERE id = NEW.observation_id;
    SELECT count(*) * jsonb_array_length(enabled_media_types)
      INTO expected_pairs
      FROM inventory_source_configuration_revisions AS config_revision
      JOIN inventory_source_configuration_revision_selected_libraries library
        ON library.source_id = config_revision.source_id AND library.revision = config_revision.revision
     WHERE config_revision.source_id = observation_source AND config_revision.revision = observation_revision
     GROUP BY config_revision.enabled_media_types;
    SELECT count(*) INTO actual_pairs FROM inventory_accepted_observation_coverage WHERE observation_id = NEW.observation_id;
    IF expected_pairs IS NULL OR actual_pairs <> expected_pairs THEN
        RAISE EXCEPTION 'accepted observation must prove every configured library and media type pair' USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER inventory_accepted_observation_coverage_complete
AFTER INSERT OR UPDATE OR DELETE ON inventory_accepted_observation_coverage
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_accepted_observation_coverage_complete();
CREATE TRIGGER inventory_accepted_observation_coverage_scope
BEFORE INSERT OR UPDATE ON inventory_accepted_observation_coverage
FOR EACH ROW EXECUTE FUNCTION validate_accepted_observation_scope();
CREATE TRIGGER inventory_accepted_observation_items_scope
BEFORE INSERT OR UPDATE ON inventory_accepted_observation_items
FOR EACH ROW EXECUTE FUNCTION validate_accepted_observation_scope();

CREATE FUNCTION validate_desired_source_revision_item() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM inventory_accepted_observation_items item
         WHERE item.observation_id = NEW.last_observation_id
           AND item.remote_library_id = NEW.remote_library_id
           AND item.source_item_id = NEW.source_item_id
           AND item.media_type = NEW.media_type
           AND item.title = NEW.title
           AND item.external_url = NEW.external_url
           AND item.item_revision = NEW.item_revision
    ) THEN
        RAISE EXCEPTION 'desired item must have exact accepted observation provenance' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER inventory_desired_source_revision_items_provenance
BEFORE INSERT OR UPDATE ON inventory_desired_source_revision_items
FOR EACH ROW EXECUTE FUNCTION validate_desired_source_revision_item();

CREATE FUNCTION prevent_accepted_observation_evidence_change() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN
    RAISE EXCEPTION 'accepted observation evidence is immutable' USING ERRCODE = '55000';
END $$;
CREATE TRIGGER inventory_accepted_observations_append_only BEFORE UPDATE OR DELETE ON inventory_accepted_observations FOR EACH ROW EXECUTE FUNCTION prevent_accepted_observation_evidence_change();
CREATE TRIGGER inventory_accepted_observation_coverage_append_only BEFORE UPDATE OR DELETE ON inventory_accepted_observation_coverage FOR EACH ROW EXECUTE FUNCTION prevent_accepted_observation_evidence_change();
CREATE TRIGGER inventory_accepted_observation_items_append_only BEFORE UPDATE OR DELETE ON inventory_accepted_observation_items FOR EACH ROW EXECUTE FUNCTION prevent_accepted_observation_evidence_change();
CREATE TRIGGER inventory_accepted_observation_events_append_only BEFORE UPDATE OR DELETE ON inventory_accepted_observation_events FOR EACH ROW EXECUTE FUNCTION prevent_accepted_observation_evidence_change();

COMMIT;
