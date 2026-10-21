DROP INDEX auth_sessions_user_expiry_idx;
ALTER TABLE oidc_transactions DROP COLUMN created_at;
