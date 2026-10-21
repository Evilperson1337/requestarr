ALTER TABLE catalog_items
    ADD COLUMN availability TEXT NOT NULL DEFAULT 'unknown'
    CHECK (availability IN ('unknown', 'available', 'missing'));

-- Deliberately do not infer availability from requests.status: lifecycle and
-- inventory evidence are independent, including for legacy 'available' rows.
CREATE TABLE availability_evidence (
    catalog_item_id UUID NOT NULL REFERENCES catalog_items(id) ON DELETE CASCADE,
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE CASCADE,
    source_item_id TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (catalog_item_id, source_id)
);

CREATE TABLE availability_events (
    id UUID PRIMARY KEY,
    catalog_item_id UUID NOT NULL REFERENCES catalog_items(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL UNIQUE,
    event_type TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL
);
