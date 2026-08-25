BEGIN;

-- This migration adds validation only and deliberately leaves immutable audit
-- evidence untouched on rollback.

COMMIT;
