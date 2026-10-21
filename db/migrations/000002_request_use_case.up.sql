ALTER TABLE requests ADD COLUMN version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE requests ADD COLUMN availability_state TEXT NOT NULL DEFAULT 'unknown'
    CHECK (availability_state IN ('unknown', 'missing', 'available'));
ALTER TABLE request_events ADD COLUMN aggregate_sequence BIGINT NOT NULL DEFAULT 1;
ALTER TABLE request_events ADD COLUMN operation_id UUID;
WITH sequenced AS (
    SELECT id, row_number() OVER (PARTITION BY request_id ORDER BY created_at, id) AS sequence
    FROM request_events
)
UPDATE request_events AS event
SET aggregate_sequence = sequenced.sequence
FROM sequenced
WHERE event.id = sequenced.id;
CREATE UNIQUE INDEX request_events_sequence_idx ON request_events(request_id, aggregate_sequence);
