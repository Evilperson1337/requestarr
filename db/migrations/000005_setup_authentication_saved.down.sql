BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM setup_workflow_events WHERE event_type = 'authentication_saved') THEN
        RAISE EXCEPTION 'cannot roll back migration 000005: authentication_saved audit events exist';
    END IF;
END $$;
ALTER TABLE setup_workflow_events DROP CONSTRAINT setup_workflow_events_event_type_check;
ALTER TABLE setup_workflow_events ADD CONSTRAINT setup_workflow_events_event_type_check
    CHECK (event_type IN ('general_saved'));

COMMIT;
