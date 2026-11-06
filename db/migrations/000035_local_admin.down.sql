DROP TABLE auth_audit_events;
DROP TABLE local_credentials;
DROP INDEX users_one_builtin;
ALTER TABLE users DROP COLUMN builtin;
