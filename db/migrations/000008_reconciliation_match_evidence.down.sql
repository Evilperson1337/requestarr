BEGIN;

-- Preserve reconciliation provenance: local/test rollback is allowed only
-- before any evidence row has been recorded.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM reconciliation_match_evidence) THEN
        RAISE EXCEPTION 'cannot roll back migration 000008 while reconciliation match evidence exists';
    END IF;
END $$;

DROP TABLE reconciliation_match_evidence;
ALTER TABLE inventory_items DROP CONSTRAINT inventory_items_id_source_id_key;

COMMIT;
