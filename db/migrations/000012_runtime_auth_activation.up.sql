BEGIN;

-- Activation is deliberately independent from setup_state.complete: normal
-- authentication can become usable while the remainder of the wizard remains.
CREATE TABLE auth_runtime_activation (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    authentication_revision BIGINT NOT NULL CHECK (authentication_revision >= 1),
    activated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE setup_workflow_events DROP CONSTRAINT setup_workflow_events_event_type_check;
ALTER TABLE setup_workflow_events ADD CONSTRAINT setup_workflow_events_event_type_check
    CHECK (event_type IN (
        'general_saved',
        'authentication_saved',
        'authentication_validation_reserved',
        'authentication_validation_succeeded',
        'authentication_validation_failed_required',
        'authentication_validation_failed_destination',
        'authentication_validation_failed_unreachable',
        'authentication_validation_failed_metadata',
        'authentication_validation_failed_unavailable',
        'authentication_validation_stale',
        'authentication_validation_rate_limited',
        'authentication_activated'
    ));

ALTER TABLE auth_audit_events DROP CONSTRAINT auth_audit_events_event_type_check;
ALTER TABLE auth_audit_events ADD CONSTRAINT auth_audit_events_event_type_check
    CHECK (event_type IN ('login_started', 'login_succeeded', 'login_failed', 'session_revoked', 'sessions_revoked', 'configuration_activated'));

COMMIT;
