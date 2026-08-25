BEGIN;

-- This corrective integrity migration is intentionally not reversible. Use a
-- tested backup and schema-compatible release for production rollback.
DO $$ BEGIN
    RAISE EXCEPTION '000023 dirty-v20 recovery integrity correction cannot be migrated down';
END $$;

COMMIT;
