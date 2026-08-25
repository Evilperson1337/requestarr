BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM auth_runtime_config)
       OR EXISTS (SELECT 1 FROM auth_login_transactions)
       OR EXISTS (SELECT 1 FROM auth_sessions)
       OR EXISTS (SELECT 1 FROM auth_audit_events)
       OR EXISTS (SELECT 1 FROM users) THEN
        RAISE EXCEPTION 'cannot roll back normal-auth foundation with persisted authentication data';
    END IF;
END
$$;

DROP TABLE auth_audit_events;
DROP TABLE auth_sessions;
DROP TABLE auth_login_transactions;
DROP TABLE auth_runtime_config;

ALTER TABLE users DROP CONSTRAINT users_oidc_issuer_subject_key;
ALTER TABLE users DROP COLUMN oidc_issuer;
ALTER TABLE users ADD CONSTRAINT users_oidc_subject_key UNIQUE (oidc_subject);

COMMIT;
