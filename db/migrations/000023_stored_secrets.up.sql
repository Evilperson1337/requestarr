-- Stored integration secrets (D024). The ciphertext is AES-256-GCM output under
-- the operator's master key; key_id records which key sealed it. Plaintext is
-- never stored.
CREATE TABLE stored_secrets (
    name TEXT PRIMARY KEY,
    ciphertext BYTEA NOT NULL,
    key_id TEXT NOT NULL,
    updated_by UUID NOT NULL REFERENCES users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Deliberately no value, ciphertext or key material in audit.
CREATE TABLE secret_audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_user_id UUID NOT NULL REFERENCES users(id),
    secret_name TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('set', 'clear')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
