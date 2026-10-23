DROP TABLE setup_draft_events;

ALTER TABLE setup_state
    DROP COLUMN draft_validations,
    DROP COLUMN draft_revision;
