BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM setup_authentication_validation_evidence)
        OR EXISTS (SELECT 1 FROM setup_authentication_validation_rate_limits)
        OR EXISTS (
            SELECT 1 FROM setup_workflow_events
            WHERE event_type LIKE 'authentication_validation_%'
        ) THEN
        RAISE EXCEPTION 'refusing to remove OIDC validation evidence, rate state, or audit history';
    END IF;
END $$;

ALTER TABLE setup_workflow_events DROP CONSTRAINT setup_workflow_events_event_type_check;
ALTER TABLE setup_workflow_events ADD CONSTRAINT setup_workflow_events_event_type_check
    CHECK (event_type IN ('general_saved', 'authentication_saved'));

DROP TABLE setup_authentication_validation_rate_limits;
DROP TABLE setup_authentication_validation_evidence;
ALTER TABLE setup_state DROP COLUMN authentication_revision;

COMMIT;
