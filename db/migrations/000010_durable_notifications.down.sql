-- DATA LOSS: rollback removes user Apprise preferences and notification title,
-- body, event-key, read, lease, scheduling, and update metadata. Export any
-- required notification data before applying this downgrade.
DROP TABLE notification_preferences;
DROP INDEX notification_deliveries_outbox_idx;
DROP INDEX notification_deliveries_inbox_idx;
DROP INDEX notification_deliveries_event_user_channel_idx;
ALTER TABLE notification_deliveries
    DROP COLUMN updated_at,
    DROP COLUMN lease_token,
    DROP COLUMN lease_until,
    DROP COLUMN available_at,
    DROP COLUMN read_at,
    DROP COLUMN body,
    DROP COLUMN title,
    DROP COLUMN event_key;
