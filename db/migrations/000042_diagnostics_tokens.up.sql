-- A diagnostics token (D043) lets a tool read a short fixed list of
-- superuser-only routes without a session. The token itself is never stored:
-- token_hash is an HMAC-SHA256 under the session secret, as sessions are.
CREATE TABLE diagnostics_tokens (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > created_at),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    token_hash BYTEA NOT NULL UNIQUE
);
CREATE INDEX diagnostics_tokens_created_by_idx ON diagnostics_tokens (created_by);

-- Creating and revoking a token are audited with the actor and the token's id,
-- never its value.
ALTER TABLE auth_audit_events DROP CONSTRAINT auth_audit_events_event_type_check;
ALTER TABLE auth_audit_events ADD CONSTRAINT auth_audit_events_event_type_check
    CHECK (event_type IN ('signin_success','signin_failure','signin_throttled','password_changed','password_change_failure','admin_created','diagnostics_token_created','diagnostics_token_revoked'));
ALTER TABLE auth_audit_events ADD COLUMN token_id UUID REFERENCES diagnostics_tokens(id) ON DELETE SET NULL;
