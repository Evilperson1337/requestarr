-- Stable pagination snapshots hold IDs/order only, never stale request details.
-- The cursor signing key is local to one snapshot and must never be logged.
CREATE TABLE request_list_snapshots (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    query_key TEXT NOT NULL,
    cursor_key BYTEA NOT NULL CHECK (octet_length(cursor_key)=32),
    request_ids UUID[] NOT NULL CHECK (cardinality(request_ids) BETWEEN 1 AND 10000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (clock_timestamp() + interval '15 minutes')
);
CREATE INDEX request_list_snapshots_user_idx ON request_list_snapshots(user_id,created_at DESC,id DESC);
CREATE INDEX request_list_snapshots_expiry_idx ON request_list_snapshots(expires_at,id);
