BEGIN;

CREATE TABLE users (
    id UUID PRIMARY KEY,
    oidc_subject TEXT NOT NULL UNIQUE,
    email TEXT,
    display_name TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('viewer', 'requester', 'admin')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE catalog_items (
    id UUID PRIMARY KEY,
    media_type TEXT NOT NULL CHECK (media_type IN ('movie','tv','album','song','game')),
    provider TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    title TEXT NOT NULL,
    year INTEGER,
    artwork_url TEXT,
    artwork_kind TEXT NOT NULL CHECK (artwork_kind IN ('poster','square')),
    external_url TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata_fetched_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (provider, provider_id, media_type)
);

CREATE TABLE requests (
    id UUID PRIMARY KEY,
    catalog_item_id UUID NOT NULL UNIQUE REFERENCES catalog_items(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('requested','approved','denied','available','cancelled')),
    requested_by UUID NOT NULL REFERENCES users(id),
    requester_note TEXT,
    admin_note TEXT,
    acceptable_platforms JSONB NOT NULL DEFAULT '{"any":false,"platformKeys":[]}'::jsonb,
    requested_seasons JSONB,
    requested_at TIMESTAMPTZ NOT NULL,
    reviewed_by UUID REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    available_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX requests_status_idx ON requests(status);
CREATE INDEX requests_requested_by_idx ON requests(requested_by);
CREATE INDEX requests_requested_at_idx ON requests(requested_at DESC);

CREATE TABLE request_subscriptions (
    request_id UUID NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('requester','manual')),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (request_id, user_id)
);

CREATE TABLE request_events (
    id UUID PRIMARY KEY,
    request_id UUID NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    actor_user_id UUID REFERENCES users(id),
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX request_events_request_created_idx ON request_events(request_id, created_at DESC);

CREATE TABLE library_sources (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('plex','jellyfin','navidrome','romm')),
    name TEXT NOT NULL,
    base_url TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    authoritative_media_types JSONB NOT NULL,
    secret_ref TEXT,
    sync_cursor TEXT,
    last_sync_started_at TIMESTAMPTZ,
    last_sync_succeeded_at TIMESTAMPTZ,
    last_sync_error TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE inventory_items (
    id UUID PRIMARY KEY,
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE CASCADE,
    source_item_id TEXT NOT NULL,
    media_type TEXT NOT NULL,
    title TEXT NOT NULL,
    year INTEGER,
    platform_key TEXT,
    external_ids JSONB NOT NULL DEFAULT '{}'::jsonb,
    external_url TEXT,
    raw_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    UNIQUE (source_id, source_item_id)
);
CREATE INDEX inventory_media_type_idx ON inventory_items(media_type);
CREATE INDEX inventory_external_ids_gin_idx ON inventory_items USING GIN(external_ids);
CREATE INDEX inventory_platform_idx ON inventory_items(platform_key) WHERE platform_key IS NOT NULL;

CREATE TABLE notification_deliveries (
    id UUID PRIMARY KEY,
    request_event_id UUID REFERENCES request_events(id) ON DELETE SET NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel TEXT NOT NULL CHECK (channel IN ('in_app','apprise')),
    status TEXT NOT NULL CHECK (status IN ('pending','sent','failed','suppressed')),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    sent_at TIMESTAMPTZ
);

CREATE TABLE setup_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    complete BOOLEAN NOT NULL DEFAULT FALSE,
    current_step TEXT NOT NULL DEFAULT 'welcome',
    draft JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL
);

INSERT INTO setup_state(singleton, complete, current_step, draft, updated_at)
VALUES (TRUE, FALSE, 'welcome', '{}'::jsonb, NOW());

COMMIT;
