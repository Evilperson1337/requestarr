BEGIN;

ALTER TABLE setup_workflow_events DROP CONSTRAINT setup_workflow_events_event_type_check;
ALTER TABLE setup_workflow_events ADD CONSTRAINT setup_workflow_events_event_type_check
    CHECK (event_type IN ('general_saved', 'authentication_saved'));

COMMIT;
