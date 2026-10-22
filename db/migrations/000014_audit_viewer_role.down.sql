-- Rolling back deletes audit rows that involve the viewer role: the old constraint cannot represent them.
DELETE FROM admin_user_audit_events WHERE previous_role = 'viewer' OR new_role = 'viewer';
ALTER TABLE admin_user_audit_events DROP CONSTRAINT admin_user_audit_events_previous_role_check;
ALTER TABLE admin_user_audit_events DROP CONSTRAINT admin_user_audit_events_new_role_check;
ALTER TABLE admin_user_audit_events ADD CONSTRAINT admin_user_audit_events_previous_role_check CHECK (previous_role IN ('admin', 'requester'));
ALTER TABLE admin_user_audit_events ADD CONSTRAINT admin_user_audit_events_new_role_check CHECK (new_role IN ('admin', 'requester'));
