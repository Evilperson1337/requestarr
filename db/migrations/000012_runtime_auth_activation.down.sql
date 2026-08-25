BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM auth_runtime_activation)
       OR EXISTS (SELECT 1 FROM auth_runtime_config)
       OR EXISTS (SELECT 1 FROM setup_workflow_events WHERE event_type = 'authentication_activated')
       OR EXISTS (SELECT 1 FROM auth_audit_events WHERE event_type = 'configuration_activated') THEN
        RAISE EXCEPTION 'cannot roll back migration 000012: runtime authentication activation data exists';
    END IF;
END $$;

ALTER TABLE auth_audit_events DROP CONSTRAINT auth_audit_events_event_type_check;
ALTER TABLE auth_audit_events ADD CONSTRAINT auth_audit_events_event_type_check
    CHECK (event_type IN ('login_started', 'login_succeeded', 'login_failed', 'session_revoked', 'sessions_revoked'));

ALTER TABLE setup_workflow_events DROP CONSTRAINT setup_workflow_events_event_type_check;
ALTER TABLE setup_workflow_events ADD CONSTRAINT setup_workflow_events_event_type_check
    CHECK (event_type IN (
        'general_saved', 'authentication_saved', 'authentication_validation_reserved',
        'authentication_validation_succeeded', 'authentication_validation_failed_required',
        'authentication_validation_failed_destination', 'authentication_validation_failed_unreachable',
        'authentication_validation_failed_metadata', 'authentication_validation_failed_unavailable',
        'authentication_validation_stale', 'authentication_validation_rate_limited'
    ));

DROP TABLE auth_runtime_activation;

COMMIT;
