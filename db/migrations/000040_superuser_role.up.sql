-- A fourth role, superuser, ranks above admin. Every existing admin, the
-- built-in local administrator (D040) included, becomes a superuser, so nobody
-- loses access: the owner can then demote people who should only be admins.
-- Requesters and viewers are unchanged.
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('viewer', 'requester', 'admin', 'superuser'));
UPDATE users SET role = 'superuser' WHERE role = 'admin';
-- The built-in account is a superuser for good; the store refuses to change it and so does the database.
ALTER TABLE users ADD CONSTRAINT users_builtin_superuser CHECK (NOT builtin OR role = 'superuser');

-- The role-change audit keeps its old rows as they were (they still say admin)
-- and can now record superuser and where a change came from.
ALTER TABLE admin_user_audit_events DROP CONSTRAINT admin_user_audit_events_previous_role_check;
ALTER TABLE admin_user_audit_events DROP CONSTRAINT admin_user_audit_events_new_role_check;
ALTER TABLE admin_user_audit_events ADD CONSTRAINT admin_user_audit_events_previous_role_check CHECK (previous_role IN ('viewer', 'requester', 'admin', 'superuser'));
ALTER TABLE admin_user_audit_events ADD CONSTRAINT admin_user_audit_events_new_role_check CHECK (new_role IN ('viewer', 'requester', 'admin', 'superuser'));
ALTER TABLE admin_user_audit_events ADD COLUMN source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'group_mapping'));
