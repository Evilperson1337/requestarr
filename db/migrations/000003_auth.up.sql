CREATE TABLE oidc_identities (
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issuer, subject),
    UNIQUE (user_id)
);

CREATE TABLE auth_sessions (
    token_hash BYTEA PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_hash BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE oidc_transactions (
    state_hash BYTEA PRIMARY KEY,
    nonce TEXT NOT NULL,
    pkce_verifier TEXT NOT NULL,
    preauth_hash BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
