BEGIN;

-- A subject is only unique within its issuer. Existing installations cannot be
-- safely backfilled because the old schema did not retain issuer provenance.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM users) THEN
        RAISE EXCEPTION 'cannot apply normal-auth identity correction while users lack issuer provenance';
    END IF;
END
$$;

ALTER TABLE users DROP CONSTRAINT users_oidc_subject_key;
ALTER TABLE users
    ADD COLUMN oidc_issuer TEXT NOT NULL CHECK (octet_length(oidc_issuer) BETWEEN 1 AND 2048),
    ADD CONSTRAINT users_oidc_issuer_subject_key UNIQUE (oidc_issuer, oidc_subject);

-- This singleton is the reviewed runtime configuration snapshot. It is empty
-- until a later activation transaction copies a validated setup revision.
CREATE TABLE auth_runtime_config (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    authentication_revision BIGINT NOT NULL CHECK (authentication_revision >= 1),
    issuer TEXT NOT NULL CHECK (octet_length(issuer) BETWEEN 1 AND 2048),
    client_id TEXT NOT NULL CHECK (octet_length(client_id) BETWEEN 1 AND 512),
    client_type TEXT NOT NULL CHECK (client_type IN ('public', 'confidential')),
    scopes JSONB NOT NULL CHECK (jsonb_typeof(scopes) = 'array' AND jsonb_array_length(scopes) BETWEEN 1 AND 16 AND octet_length(scopes::text) <= 2048),
    groups_claim TEXT NOT NULL CHECK (octet_length(groups_claim) BETWEEN 1 AND 256),
    default_role TEXT NOT NULL CHECK (default_role IN ('viewer', 'requester', 'admin')),
    group_role_mappings JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(group_role_mappings) = 'array' AND jsonb_array_length(group_role_mappings) <= 128 AND octet_length(group_role_mappings::text) <= 16384),
    activated_at TIMESTAMPTZ NOT NULL
);

-- State, nonce, PKCE verifier, and browser session values are opaque random
-- values. Only domain-separated 32-byte digests are ever persisted.
CREATE TABLE auth_login_transactions (
    id UUID PRIMARY KEY,
    state_digest BYTEA NOT NULL UNIQUE CHECK (octet_length(state_digest) = 32),
    nonce_digest BYTEA NOT NULL UNIQUE CHECK (octet_length(nonce_digest) = 32),
    pkce_verifier_digest BYTEA NOT NULL UNIQUE CHECK (octet_length(pkce_verifier_digest) = 32),
    authentication_revision BIGINT NOT NULL CHECK (authentication_revision >= 1),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    CHECK (expires_at > created_at),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);
CREATE INDEX auth_login_transactions_live_expiry_idx
    ON auth_login_transactions(expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE auth_sessions (
    id UUID PRIMARY KEY,
    token_digest BYTEA NOT NULL UNIQUE CHECK (octet_length(token_digest) = 32),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    authentication_revision BIGINT NOT NULL CHECK (authentication_revision >= 1),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CHECK (expires_at > created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE INDEX auth_sessions_user_live_expiry_idx
    ON auth_sessions(user_id, expires_at)
    WHERE revoked_at IS NULL;
CREATE INDEX auth_sessions_live_expiry_idx
    ON auth_sessions(expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE auth_audit_events (
    id UUID PRIMARY KEY,
    event_type TEXT NOT NULL CHECK (event_type IN ('login_started', 'login_succeeded', 'login_failed', 'session_revoked', 'sessions_revoked')),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    session_id UUID REFERENCES auth_sessions(id) ON DELETE SET NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(data) = 'object' AND octet_length(data::text) <= 4096),
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX auth_audit_events_created_idx ON auth_audit_events(created_at DESC);
CREATE INDEX auth_audit_events_user_created_idx ON auth_audit_events(user_id, created_at DESC) WHERE user_id IS NOT NULL;

COMMIT;
