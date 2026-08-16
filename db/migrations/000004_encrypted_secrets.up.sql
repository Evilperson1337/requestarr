BEGIN;

CREATE TABLE encryption_key_registry (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    active_key_id BYTEA NOT NULL UNIQUE CHECK (octet_length(active_key_id) = 16),
    created_at TIMESTAMPTZ NOT NULL DEFAULT transaction_timestamp()
);

CREATE TABLE encrypted_secrets (
    namespace TEXT NOT NULL CHECK (namespace <> '' AND octet_length(namespace) <= 64),
    owner_id UUID NOT NULL,
    secret_name TEXT NOT NULL CHECK (secret_name <> '' AND octet_length(secret_name) <= 128),
    envelope_version SMALLINT NOT NULL CHECK (envelope_version = 1),
    key_id BYTEA NOT NULL CHECK (octet_length(key_id) = 16),
    nonce BYTEA NOT NULL CHECK (octet_length(nonce) = 12),
    ciphertext BYTEA NOT NULL CHECK (octet_length(ciphertext) BETWEEN 16 AND 65552),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (namespace, owner_id, secret_name),
    CHECK (updated_at >= created_at),
    FOREIGN KEY (key_id) REFERENCES encryption_key_registry(active_key_id)
);
CREATE INDEX encrypted_secrets_key_id_idx ON encrypted_secrets(key_id);

COMMIT;
