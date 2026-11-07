-- Rolling back turns every superuser into an admin (the old top role) and
-- forgets where audited changes came from.
ALTER TABLE users DROP CONSTRAINT users_builtin_superuser;
UPDATE users SET role = 'admin' WHERE role = 'superuser';
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('viewer', 'requester', 'admin'));

ALTER TABLE admin_user_audit_events DROP COLUMN source;
UPDATE admin_user_audit_events SET previous_role = 'admin' WHERE previous_role = 'superuser';
UPDATE admin_user_audit_events SET new_role = 'admin' WHERE new_role = 'superuser';
ALTER TABLE admin_user_audit_events DROP CONSTRAINT admin_user_audit_events_previous_role_check;
ALTER TABLE admin_user_audit_events DROP CONSTRAINT admin_user_audit_events_new_role_check;
ALTER TABLE admin_user_audit_events ADD CONSTRAINT admin_user_audit_events_previous_role_check CHECK (previous_role IN ('viewer', 'requester', 'admin'));
ALTER TABLE admin_user_audit_events ADD CONSTRAINT admin_user_audit_events_new_role_check CHECK (new_role IN ('viewer', 'requester', 'admin'));
