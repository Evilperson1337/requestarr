-- Preserve 000010 for migration history; legacy per-user Apprise preferences and
-- deliveries are deprecated. External delivery is now a single global event.
CREATE TABLE notification_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_key TEXT NOT NULL UNIQUE CHECK (length(event_key) BETWEEN 1 AND 256),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 512),
    body TEXT NOT NULL CHECK (length(body) <= 2048),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sent','failed')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ,
    lease_token UUID,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ
);
CREATE INDEX notification_outbox_claim_idx ON notification_outbox(available_at, created_at)
    WHERE status = 'pending';
