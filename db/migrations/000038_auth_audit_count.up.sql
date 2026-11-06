-- Failed and throttled sign-in attempts are coalesced to one row per account or
-- address per minute (D040); count is how many attempts a row stands for.
ALTER TABLE auth_audit_events ADD COLUMN count INTEGER NOT NULL DEFAULT 1 CHECK (count >= 1);
