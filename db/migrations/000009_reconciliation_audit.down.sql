BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM reconciliation_match_events) THEN
        RAISE EXCEPTION 'cannot roll back reconciliation audit with persisted event history';
    END IF;
END
$$;

DROP TABLE reconciliation_match_events;

COMMIT;
