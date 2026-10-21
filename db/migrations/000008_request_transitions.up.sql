-- Operation IDs are audit correlation keys, not replay tokens. A key may be
-- reused after a failed transaction because no event was committed.
CREATE UNIQUE INDEX request_events_operation_id_idx
    ON request_events(operation_id) WHERE operation_id IS NOT NULL;
