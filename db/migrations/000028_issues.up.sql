-- Issues: reports that a Library item has a problem (D030). Never a Request.
CREATE TABLE issues (
    id                UUID PRIMARY KEY,
    inventory_item_id UUID NOT NULL REFERENCES inventory_items(id),
    kind              TEXT NOT NULL,
    part              JSONB NOT NULL DEFAULT '{}'::jsonb,
    part_key          TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','closed')),
    created_by        UUID NOT NULL REFERENCES users(id),
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    resolved_by       UUID REFERENCES users(id),
    resolved_at       TIMESTAMPTZ,
    version           BIGINT NOT NULL DEFAULT 1
);
-- One shared open Issue per Library item, kind and part.
CREATE UNIQUE INDEX issues_one_open ON issues (inventory_item_id, kind, part_key) WHERE status = 'open';
CREATE INDEX issues_by_status ON issues (status, created_at DESC);

CREATE TABLE issue_reporters (
    issue_id   UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id),
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    left_at    TIMESTAMPTZ,
    PRIMARY KEY (issue_id, user_id)
);

CREATE TABLE issue_events (
    id            UUID PRIMARY KEY,
    issue_id      UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    event_type    TEXT NOT NULL CHECK (event_type IN ('reported','reporter_added','commented','resolved','closed','reopened','left')),
    actor_user_id UUID NOT NULL REFERENCES users(id),
    comment       TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL
);
CREATE INDEX issue_events_by_issue ON issue_events (issue_id, created_at);
CREATE INDEX issue_events_rate ON issue_events (actor_user_id, created_at) WHERE event_type IN ('reported','reporter_added');
