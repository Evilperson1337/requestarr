ALTER TABLE admin_user_audit_events DROP CONSTRAINT admin_user_audit_events_previous_role_check;
ALTER TABLE admin_user_audit_events DROP CONSTRAINT admin_user_audit_events_new_role_check;
ALTER TABLE admin_user_audit_events ADD CONSTRAINT admin_user_audit_events_previous_role_check CHECK (previous_role IN ('viewer', 'requester', 'admin'));
ALTER TABLE admin_user_audit_events ADD CONSTRAINT admin_user_audit_events_new_role_check CHECK (new_role IN ('viewer', 'requester', 'admin'));
