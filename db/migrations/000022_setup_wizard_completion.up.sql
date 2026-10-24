ALTER TABLE setup_state
    ADD COLUMN wizard_completed_at TIMESTAMPTZ,
    ADD COLUMN wizard_completed_by UUID REFERENCES users(id),
    ADD CONSTRAINT setup_state_wizard_completion_pair_check
        CHECK ((wizard_completed_at IS NULL) = (wizard_completed_by IS NULL));

ALTER TABLE setup_draft_events
    DROP CONSTRAINT setup_draft_events_event_type_check,
    ADD CONSTRAINT setup_draft_events_event_type_check
        CHECK (event_type IN ('initialized', 'saved', 'validated', 'completed'));
