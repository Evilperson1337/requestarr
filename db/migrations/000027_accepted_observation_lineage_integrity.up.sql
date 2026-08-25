BEGIN;

-- v26 introduced the columns needed to describe lineage, but its constraints
-- only certified the current head.  Make all server-owned acceptance metadata
-- explicit so direct INSERT statements cannot select a historical position or
-- an audit timestamp.
ALTER TABLE inventory_accepted_observations
    ALTER COLUMN accepted_at DROP DEFAULT,
    ALTER COLUMN acceptance_order DROP IDENTITY IF EXISTS,
    ALTER COLUMN acceptance_order DROP DEFAULT;

CREATE SEQUENCE inventory_accepted_observation_acceptance_order_seq;
ALTER SEQUENCE inventory_accepted_observation_acceptance_order_seq
    OWNED BY inventory_accepted_observations.acceptance_order;
SELECT setval(
    'inventory_accepted_observation_acceptance_order_seq',
    COALESCE(max(acceptance_order), 1),
    max(acceptance_order) IS NOT NULL
)
FROM inventory_accepted_observations;

CREATE OR REPLACE FUNCTION assign_accepted_observation_metadata() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- Defaults are intentionally absent: a caller-provided value is observable
    -- here and rejected rather than silently becoming audit evidence.
    IF NEW.accepted_at IS NOT NULL OR NEW.acceptance_order IS NOT NULL
       OR NEW.predecessor_observation_id IS NOT NULL THEN
        RAISE EXCEPTION 'accepted observation acceptance metadata is assigned by the database' USING ERRCODE = '23514';
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.source_id::text, 0));
    NEW.accepted_at := statement_timestamp();
    NEW.acceptance_order := nextval('inventory_accepted_observation_acceptance_order_seq');
    SELECT id INTO NEW.predecessor_observation_id
      FROM inventory_accepted_observations
     WHERE source_id = NEW.source_id AND configuration_revision = NEW.configuration_revision
     ORDER BY acceptance_order DESC
     LIMIT 1;
    RETURN NEW;
END $$;

-- Backfill the new provenance metadata before installing strict validation.
-- Existing observation rows remain immutable after this one-time certification
-- migration; only the v26 metadata column and mutable desired projection move.
ALTER TABLE inventory_accepted_observations DISABLE TRIGGER USER;
UPDATE inventory_accepted_observations observation
   SET predecessor_observation_id = predecessor.id
  FROM LATERAL (
      SELECT prior.id
        FROM inventory_accepted_observations prior
       WHERE prior.source_id = observation.source_id
         AND prior.configuration_revision = observation.configuration_revision
         AND prior.acceptance_order < observation.acceptance_order
       ORDER BY prior.acceptance_order DESC
       LIMIT 1
  ) predecessor;
ALTER TABLE inventory_accepted_observations ENABLE TRIGGER USER;

-- A desired row always cites the observation that most recently accepted that
-- item, including unchanged items.  Fail later certification if its values do
-- not actually match that head observation.
UPDATE inventory_desired_source_revision_items desired
   SET last_observation_id = latest.id
  FROM LATERAL (
      SELECT observation.id
        FROM inventory_accepted_observations observation
       WHERE observation.source_id = desired.source_id
         AND observation.configuration_revision = desired.configuration_revision
       ORDER BY observation.acceptance_order DESC
       LIMIT 1
  ) latest;

CREATE OR REPLACE FUNCTION validate_accepted_observation_transition(observation UUID) RETURNS VOID
LANGUAGE plpgsql AS $$
DECLARE
    source UUID;
    revision BIGINT;
    predecessor UUID;
    acceptance BIGINT;
    expected_predecessor UUID;
    latest UUID;
    expected_coverage INTEGER;
BEGIN
    SELECT source_id, configuration_revision, predecessor_observation_id, acceptance_order
      INTO source, revision, predecessor, acceptance
      FROM inventory_accepted_observations WHERE id = observation;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    IF acceptance <= 0 THEN
        RAISE EXCEPTION 'accepted observation acceptance order must be strictly positive' USING ERRCODE = '23514';
    END IF;

    SELECT count(*) * jsonb_array_length(enabled_media_types)
      INTO expected_coverage
      FROM inventory_source_configuration_revisions r
      JOIN inventory_source_configuration_revision_selected_libraries l
        ON l.source_id = r.source_id AND l.revision = r.revision
     WHERE r.source_id = source AND r.revision = revision
     GROUP BY r.enabled_media_types;
    IF expected_coverage IS NULL OR
       (SELECT count(*) FROM inventory_accepted_observation_coverage WHERE observation_id = observation) <> expected_coverage THEN
        RAISE EXCEPTION 'accepted observation must prove every configured library and media type pair' USING ERRCODE = '23514';
    END IF;

    SELECT id INTO expected_predecessor
      FROM inventory_accepted_observations
     WHERE source_id = source AND configuration_revision = revision AND acceptance_order < acceptance
     ORDER BY acceptance_order DESC
     LIMIT 1;
    IF predecessor IS DISTINCT FROM expected_predecessor THEN
        RAISE EXCEPTION 'accepted observation predecessor is not the prior accepted state' USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        (SELECT 'added'::TEXT, n.remote_library_id, n.source_item_id, n.media_type
           FROM inventory_accepted_observation_items n
          WHERE n.observation_id = observation
            AND NOT EXISTS (SELECT 1 FROM inventory_accepted_observation_items o WHERE o.observation_id IS NOT DISTINCT FROM predecessor AND o.remote_library_id = n.remote_library_id AND o.source_item_id = n.source_item_id)
         EXCEPT SELECT change_kind, remote_library_id, source_item_id, media_type FROM inventory_accepted_observation_events WHERE observation_id = observation)
        UNION ALL
        (SELECT 'updated'::TEXT, n.remote_library_id, n.source_item_id, n.media_type
           FROM inventory_accepted_observation_items n JOIN inventory_accepted_observation_items o ON o.observation_id IS NOT DISTINCT FROM predecessor AND o.remote_library_id = n.remote_library_id AND o.source_item_id = n.source_item_id
          WHERE n.observation_id = observation AND (n.media_type, n.title, n.external_url, n.item_revision) IS DISTINCT FROM (o.media_type, o.title, o.external_url, o.item_revision)
         EXCEPT SELECT change_kind, remote_library_id, source_item_id, media_type FROM inventory_accepted_observation_events WHERE observation_id = observation)
        UNION ALL
        (SELECT 'removed'::TEXT, o.remote_library_id, o.source_item_id, o.media_type
           FROM inventory_accepted_observation_items o
          WHERE o.observation_id IS NOT DISTINCT FROM predecessor
            AND NOT EXISTS (SELECT 1 FROM inventory_accepted_observation_items n WHERE n.observation_id = observation AND n.remote_library_id = o.remote_library_id AND n.source_item_id = o.source_item_id)
         EXCEPT SELECT change_kind, remote_library_id, source_item_id, media_type FROM inventory_accepted_observation_events WHERE observation_id = observation)
        UNION ALL
        (SELECT change_kind, remote_library_id, source_item_id, media_type FROM inventory_accepted_observation_events WHERE observation_id = observation
         EXCEPT
         (SELECT 'added'::TEXT, n.remote_library_id, n.source_item_id, n.media_type FROM inventory_accepted_observation_items n WHERE n.observation_id = observation AND NOT EXISTS (SELECT 1 FROM inventory_accepted_observation_items o WHERE o.observation_id IS NOT DISTINCT FROM predecessor AND o.remote_library_id = n.remote_library_id AND o.source_item_id = n.source_item_id)
          UNION ALL SELECT 'updated'::TEXT, n.remote_library_id, n.source_item_id, n.media_type FROM inventory_accepted_observation_items n JOIN inventory_accepted_observation_items o ON o.observation_id IS NOT DISTINCT FROM predecessor AND o.remote_library_id = n.remote_library_id AND o.source_item_id = n.source_item_id WHERE n.observation_id = observation AND (n.media_type, n.title, n.external_url, n.item_revision) IS DISTINCT FROM (o.media_type, o.title, o.external_url, o.item_revision)
          UNION ALL SELECT 'removed'::TEXT, o.remote_library_id, o.source_item_id, o.media_type FROM inventory_accepted_observation_items o WHERE o.observation_id IS NOT DISTINCT FROM predecessor AND NOT EXISTS (SELECT 1 FROM inventory_accepted_observation_items n WHERE n.observation_id = observation AND n.remote_library_id = o.remote_library_id AND n.source_item_id = o.source_item_id)))
    ) THEN
        RAISE EXCEPTION 'accepted observation events must exactly prove the deterministic transition' USING ERRCODE = '23514';
    END IF;
    IF EXISTS (SELECT 1 FROM (SELECT ordinal, row_number() OVER (ORDER BY CASE change_kind WHEN 'added' THEN 1 WHEN 'updated' THEN 2 ELSE 3 END, remote_library_id, source_item_id) expected_ordinal FROM inventory_accepted_observation_events WHERE observation_id = observation) ordered_events WHERE ordinal <> expected_ordinal) THEN
        RAISE EXCEPTION 'accepted observation event ordinals must use deterministic transition order' USING ERRCODE = '23514';
    END IF;

    SELECT id INTO latest FROM inventory_accepted_observations
     WHERE source_id = source AND configuration_revision = revision
     ORDER BY acceptance_order DESC LIMIT 1;
    IF latest = observation AND EXISTS (
        (SELECT remote_library_id, source_item_id, media_type, title, external_url, item_revision
           FROM inventory_accepted_observation_items WHERE observation_id = observation
         EXCEPT SELECT remote_library_id, source_item_id, media_type, title, external_url, item_revision
           FROM inventory_desired_source_revision_items WHERE source_id = source AND configuration_revision = revision)
        UNION ALL
        (SELECT remote_library_id, source_item_id, media_type, title, external_url, item_revision
           FROM inventory_desired_source_revision_items WHERE source_id = source AND configuration_revision = revision
         EXCEPT SELECT remote_library_id, source_item_id, media_type, title, external_url, item_revision
           FROM inventory_accepted_observation_items WHERE observation_id = observation)
        UNION ALL
        (SELECT NULL::TEXT, NULL::TEXT, NULL::TEXT, NULL::TEXT, NULL::TEXT, NULL::TEXT
           WHERE EXISTS (SELECT 1 FROM inventory_desired_source_revision_items WHERE source_id = source AND configuration_revision = revision AND last_observation_id <> observation))
    ) THEN
        RAISE EXCEPTION 'desired projection must exactly equal and cite the latest accepted observation evidence' USING ERRCODE = '23514';
    END IF;
END $$;

CREATE TRIGGER inventory_accepted_observations_assign_metadata
BEFORE INSERT ON inventory_accepted_observations
FOR EACH ROW EXECUTE FUNCTION assign_accepted_observation_metadata();

-- Validate every historical record, not merely the current head.  This fails
-- the upgrade atomically if v25/v26 evidence cannot be certified.
DO $$
DECLARE observation UUID;
BEGIN
    FOR observation IN SELECT id FROM inventory_accepted_observations LOOP
        PERFORM validate_accepted_observation_transition(observation);
    END LOOP;
END $$;

COMMIT;
