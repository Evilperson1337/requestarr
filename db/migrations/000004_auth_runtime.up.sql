-- Runtime fields remain separate from the unmerged auth foundation migration.
ALTER TABLE oidc_transactions ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();
CREATE INDEX auth_sessions_user_expiry_idx ON auth_sessions(user_id, expires_at);
