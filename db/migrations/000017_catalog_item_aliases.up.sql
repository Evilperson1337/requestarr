-- Alternate identifiers that name an existing catalog item. A Source may expose
-- an identifier that is not the catalog item's canonical one (Plex exposes
-- MusicBrainz release and release-track MBIDs; the catalog identifies an album
-- by its release-group and a song by its recording). Matching a Source item to
-- the catalog also consults this table.
CREATE TABLE catalog_item_aliases (
    catalog_item_id UUID NOT NULL REFERENCES catalog_items(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    value TEXT NOT NULL CHECK (length(value) BETWEEN 1 AND 256),
    PRIMARY KEY (catalog_item_id, kind, value)
);
CREATE INDEX catalog_item_aliases_lookup_idx ON catalog_item_aliases(kind, value);

-- When a MusicBrainz alias lookup last failed for an item. Failed items wait out
-- a backoff and queue behind items never tried, so one permanently failing
-- lookup (a deleted MBID) cannot starve newer Requests.
CREATE TABLE catalog_item_alias_attempts (
    catalog_item_id UUID PRIMARY KEY REFERENCES catalog_items(id) ON DELETE CASCADE,
    last_attempt_at TIMESTAMPTZ NOT NULL
);
