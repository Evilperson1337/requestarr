ALTER TABLE setup_state
    ADD COLUMN draft_revision BIGINT NOT NULL DEFAULT 0 CHECK (draft_revision >= 0),
    ADD COLUMN draft_validations JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(draft_validations) = 'array');

-- Deliberately no settings, validation messages, or credential values in audit.
CREATE TABLE setup_draft_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_user_id UUID NOT NULL REFERENCES users(id),
    revision BIGINT NOT NULL CHECK (revision > 0),
    event_type TEXT NOT NULL CHECK (event_type IN ('initialized', 'saved', 'validated')),
    target TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
