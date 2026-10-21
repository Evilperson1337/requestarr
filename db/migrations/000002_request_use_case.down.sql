BEGIN;

DROP INDEX IF EXISTS request_events_sequence_idx;
ALTER TABLE request_events DROP COLUMN IF EXISTS operation_id;
ALTER TABLE request_events DROP COLUMN IF EXISTS aggregate_sequence;
ALTER TABLE requests DROP COLUMN IF EXISTS availability_state;
ALTER TABLE requests DROP COLUMN IF EXISTS version;

COMMIT;
