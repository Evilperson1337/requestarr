BEGIN;

CREATE TABLE setup_sessions (
    id UUID PRIMARY KEY,
    token_digest BYTEA NOT NULL UNIQUE CHECK (octet_length(token_digest) = 32),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CHECK (expires_at > created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE INDEX setup_sessions_expires_at_idx ON setup_sessions(expires_at);
CREATE INDEX setup_sessions_unrevoked_expires_at_idx ON setup_sessions(expires_at) WHERE revoked_at IS NULL;

CREATE TABLE setup_exchange_rate_limits (
    key_digest BYTEA NOT NULL CHECK (octet_length(key_digest) = 32),
    scope TEXT NOT NULL CHECK (scope IN ('peer', 'global')),
    window_start TIMESTAMPTZ NOT NULL,
    failure_count INTEGER NOT NULL CHECK (failure_count >= 1),
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope, key_digest, window_start)
);
CREATE INDEX setup_exchange_rate_limits_window_start_idx ON setup_exchange_rate_limits(window_start);

CREATE TABLE setup_security_events (
    id UUID PRIMARY KEY,
    event_type TEXT NOT NULL CHECK (event_type IN (
        'session_issued',
        'session_revoked',
        'sessions_revoked_on_completion',
        'rate_limit_activated',
        'setup_completed'
    )),
    session_id UUID REFERENCES setup_sessions(id) ON DELETE SET NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX setup_security_events_created_at_idx ON setup_security_events(created_at DESC);

COMMIT;
