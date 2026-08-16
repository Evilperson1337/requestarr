BEGIN;

CREATE TABLE setup_workflow_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type TEXT NOT NULL CHECK (event_type IN ('general_saved')),
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX setup_workflow_events_created_at_idx ON setup_workflow_events(created_at DESC);

COMMIT;
