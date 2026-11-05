-- The built-in local administrator (D040): one account that signs in with a
-- password. It is an ordinary users row with the admin role, marked builtin,
-- whose oidc_subject is the fixed key 'local:admin' (OIDC keys all start with
-- 'oidc:', so the two cannot collide). Its credential lives in its own table.
ALTER TABLE users ADD COLUMN builtin BOOLEAN NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX users_one_builtin ON users (builtin) WHERE builtin;

CREATE TABLE local_credentials (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    username TEXT NOT NULL UNIQUE CHECK (username = lower(username) AND length(username) BETWEEN 1 AND 64),
    -- "pbkdf2-sha256$<iterations>$<salt>$<hash>": the scheme and cost travel with the hash.
    password_hash TEXT NOT NULL CHECK (length(password_hash) BETWEEN 1 AND 512),
    must_change BOOLEAN NOT NULL DEFAULT TRUE,
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    password_changed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Every sign-in success and failure, password change and creation. No password
-- material is ever stored here. username is set only when it names a real account.
CREATE TABLE auth_audit_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type TEXT NOT NULL CHECK (event_type IN ('signin_success','signin_failure','signin_throttled','password_changed','password_change_failure','admin_created')),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    username TEXT CHECK (username IS NULL OR length(username) <= 64),
    remote_addr TEXT CHECK (remote_addr IS NULL OR length(remote_addr) <= 64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX auth_audit_events_created_idx ON auth_audit_events (created_at DESC);
