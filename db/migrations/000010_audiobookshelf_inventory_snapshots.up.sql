BEGIN;

-- Remote libraries observed in a complete source enumeration are retained as
-- local provenance. This phase does not add selection configuration or network
-- wiring; an adapter supplies only the libraries it actually enumerated.
CREATE TABLE audiobook_source_libraries (
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE CASCADE,
    remote_library_id TEXT NOT NULL CHECK (octet_length(remote_library_id) BETWEEN 1 AND 256),
    name TEXT NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 512),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (source_id, remote_library_id)
);

-- Successful snapshots are immutable history. The content hash makes retries
-- idempotent without storing source payloads or credentials.
CREATE TABLE audiobook_inventory_snapshots (
    id UUID PRIMARY KEY,
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE RESTRICT,
    content_hash TEXT NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    completed_at TIMESTAMPTZ NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    UNIQUE (source_id, content_hash)
);
CREATE INDEX audiobook_inventory_snapshots_source_completed_idx
    ON audiobook_inventory_snapshots(source_id, completed_at DESC);

ALTER TABLE inventory_items
    ADD COLUMN last_snapshot_id UUID REFERENCES audiobook_inventory_snapshots(id) ON DELETE RESTRICT,
    ADD COLUMN tombstoned_at TIMESTAMPTZ,
    ADD CONSTRAINT inventory_items_tombstone_snapshot_check CHECK (
        (tombstoned_at IS NULL) OR last_snapshot_id IS NOT NULL
    );
CREATE INDEX inventory_items_source_live_snapshot_idx
    ON inventory_items(source_id, last_snapshot_id)
    WHERE tombstoned_at IS NULL;

-- Details are one typed constrained projection per inventory item, avoiding an
-- unbounded provider payload while preserving edition evidence for later use.
CREATE TABLE audiobook_inventory_details (
    inventory_item_id UUID PRIMARY KEY REFERENCES inventory_items(id) ON DELETE CASCADE,
    asin TEXT,
    audible_region TEXT,
    authors JSONB NOT NULL DEFAULT '[]'::jsonb,
    narrators JSONB NOT NULL DEFAULT '[]'::jsonb,
    series TEXT NOT NULL DEFAULT '',
    series_position TEXT NOT NULL DEFAULT '',
    runtime_seconds INTEGER NOT NULL DEFAULT 0 CHECK (runtime_seconds BETWEEN 0 AND 604800),
    language TEXT NOT NULL DEFAULT '',
    publisher TEXT NOT NULL DEFAULT '',
    release_date DATE,
    edition_type TEXT NOT NULL DEFAULT '',
    abridged BOOLEAN,
    alternate_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    CHECK ((asin IS NULL) = (audible_region IS NULL)),
    CHECK (asin IS NULL OR asin ~ '^[A-Z0-9]{10}$'),
    CHECK (audible_region IS NULL OR audible_region ~ '^[A-Z]{2}$'),
    CHECK (jsonb_typeof(authors) = 'array' AND jsonb_array_length(authors) <= 32 AND octet_length(authors::text) <= 8192),
    CHECK (jsonb_typeof(narrators) = 'array' AND jsonb_array_length(narrators) <= 32 AND octet_length(narrators::text) <= 8192),
    CHECK (jsonb_typeof(alternate_ids) = 'array' AND jsonb_array_length(alternate_ids) <= 16 AND octet_length(alternate_ids::text) <= 4096),
    CHECK (octet_length(series) <= 256 AND octet_length(series_position) <= 64 AND octet_length(language) <= 35 AND octet_length(publisher) <= 256 AND octet_length(edition_type) <= 64)
);

COMMIT;
