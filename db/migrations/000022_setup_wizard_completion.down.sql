DELETE FROM setup_draft_events WHERE event_type = 'completed';

ALTER TABLE setup_draft_events
    DROP CONSTRAINT setup_draft_events_event_type_check,
    ADD CONSTRAINT setup_draft_events_event_type_check
        CHECK (event_type IN ('initialized', 'saved', 'validated'));

ALTER TABLE setup_state
    DROP CONSTRAINT setup_state_wizard_completion_pair_check,
    DROP COLUMN wizard_completed_by,
    DROP COLUMN wizard_completed_at;
