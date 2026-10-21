ALTER TABLE notification_deliveries
    ADD COLUMN event_key TEXT,
    ADD COLUMN title TEXT NOT NULL DEFAULT '',
    ADD COLUMN body TEXT NOT NULL DEFAULT '',
    ADD COLUMN read_at TIMESTAMPTZ,
    ADD COLUMN available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN lease_until TIMESTAMPTZ,
    ADD COLUMN lease_token UUID,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE UNIQUE INDEX notification_deliveries_event_user_channel_idx
    ON notification_deliveries(event_key, user_id, channel) WHERE event_key IS NOT NULL;
CREATE INDEX notification_deliveries_inbox_idx
    ON notification_deliveries(user_id, created_at DESC) WHERE channel = 'in_app';
CREATE INDEX notification_deliveries_outbox_idx
    ON notification_deliveries(available_at, created_at)
    WHERE channel = 'apprise' AND status = 'pending';

ALTER TABLE notification_deliveries
    ADD CONSTRAINT notification_deliveries_event_key_nonempty
    CHECK (event_key IS NULL OR length(event_key) BETWEEN 1 AND 256);

CREATE TABLE notification_preferences (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    apprise_target TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (length(apprise_target) <= 2048)
);
