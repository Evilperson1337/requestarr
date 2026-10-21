-- Migration 000007 is still unshipped and has never been applied in production.
-- It therefore corrects its initial definition in place: 000001 owns and seeds
-- setup_state, so this migration only adds bootstrap-specific fields.
ALTER TABLE setup_state
    ADD COLUMN target_issuer TEXT NOT NULL DEFAULT '',
    ADD COLUMN target_subject TEXT NOT NULL DEFAULT '',
    ADD COLUMN token_hash BYTEA,
    ADD COLUMN token_expires_at TIMESTAMPTZ,
    ADD COLUMN claimed_by UUID REFERENCES users(id),
    ADD COLUMN completed_at TIMESTAMPTZ,
    ADD CONSTRAINT setup_state_token_expiry_pair_check
        CHECK ((token_hash IS NULL) = (token_expires_at IS NULL));

CREATE TABLE setup_audit_events (
    id UUID PRIMARY KEY,
    event_type TEXT NOT NULL CHECK (event_type IN ('admin_token_issued', 'admin_claimed')),
    actor_user_id UUID REFERENCES users(id),
    target_issuer TEXT NOT NULL,
    target_subject TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
