BEGIN;

-- Retaining accepted evidence is mandatory. Refuse rollback rather than making
-- a populated audit trail mutable or dropping its projection guarantees.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM inventory_accepted_observations)
       OR EXISTS (SELECT 1 FROM inventory_accepted_observation_coverage)
       OR EXISTS (SELECT 1 FROM inventory_accepted_observation_items)
       OR EXISTS (SELECT 1 FROM inventory_accepted_observation_events) THEN
        RAISE EXCEPTION 'cannot roll back accepted observation integrity while accepted evidence exists' USING ERRCODE = '55000';
    END IF;
END $$;

DROP TRIGGER inventory_desired_source_revision_items_complete ON inventory_desired_source_revision_items;
DROP FUNCTION validate_desired_projection_change();
DROP TRIGGER inventory_accepted_observation_events_scope ON inventory_accepted_observation_events;
DROP TRIGGER inventory_accepted_observation_events_transition ON inventory_accepted_observation_events;
DROP TRIGGER inventory_accepted_observation_items_transition ON inventory_accepted_observation_items;
DROP TRIGGER inventory_accepted_observations_complete ON inventory_accepted_observations;
DROP FUNCTION validate_accepted_observation_transition_trigger();
DROP FUNCTION validate_accepted_observation_transition(UUID);
DROP INDEX inventory_accepted_observations_scope_order_key;
ALTER TABLE inventory_accepted_observations DROP COLUMN predecessor_observation_id;
ALTER TABLE inventory_accepted_observations DROP COLUMN acceptance_order;
ALTER TABLE inventory_accepted_observations ALTER COLUMN accepted_at DROP DEFAULT;

COMMIT;
