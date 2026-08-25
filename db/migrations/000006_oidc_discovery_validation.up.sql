BEGIN;

ALTER TABLE setup_state
    ADD COLUMN authentication_revision BIGINT NOT NULL DEFAULT 0
        CHECK (authentication_revision >= 0);

UPDATE setup_state
SET authentication_revision = CASE
    WHEN jsonb_typeof(draft) = 'object'
        AND draft ->> 'version' = '2'
        AND jsonb_typeof(draft -> 'authentication') = 'object'
    THEN 1
    ELSE 0
END;

CREATE TABLE setup_authentication_validation_evidence (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    authentication_revision BIGINT NOT NULL CHECK (authentication_revision > 0),
    policy_version INTEGER NOT NULL CHECK (policy_version > 0),
    metadata_digest BYTEA NOT NULL CHECK (octet_length(metadata_digest) = 32),
    validated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (singleton) REFERENCES setup_state(singleton) ON DELETE RESTRICT
);

CREATE TABLE setup_authentication_validation_rate_limits (
    scope TEXT NOT NULL CHECK (scope IN ('session', 'global')),
    key_digest BYTEA NOT NULL CHECK (octet_length(key_digest) = 32),
    window_start TIMESTAMPTZ NOT NULL,
    attempt_count INTEGER NOT NULL CHECK (attempt_count >= 1),
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope, key_digest, window_start)
);
CREATE INDEX setup_authentication_validation_rate_window_idx
    ON setup_authentication_validation_rate_limits(window_start);

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
        'authentication_validation_rate_limited'
    ));

COMMIT;
