DELETE FROM auth_audit_events WHERE event_type IN ('diagnostics_token_created','diagnostics_token_revoked');
ALTER TABLE auth_audit_events DROP COLUMN token_id;
ALTER TABLE auth_audit_events DROP CONSTRAINT auth_audit_events_event_type_check;
ALTER TABLE auth_audit_events ADD CONSTRAINT auth_audit_events_event_type_check
    CHECK (event_type IN ('signin_success','signin_failure','signin_throttled','password_changed','password_change_failure','admin_created'));
DROP TABLE diagnostics_tokens;
