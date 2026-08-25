BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM inventory_manual_sync_request_attempts) THEN
        RAISE EXCEPTION 'manual sync request attempt evidence exists and cannot be removed'
            USING ERRCODE = '23514';
    END IF;
END $$;
DROP TABLE inventory_manual_sync_request_attempts;

COMMIT;
