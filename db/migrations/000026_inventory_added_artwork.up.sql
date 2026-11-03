-- When the library added an item, and the adapter's reference to its cover (D029).
ALTER TABLE inventory_items ADD COLUMN added_at TIMESTAMPTZ;
ALTER TABLE inventory_items ADD COLUMN artwork_ref TEXT NOT NULL DEFAULT '';
CREATE INDEX inventory_items_recent ON inventory_items (source_id, (COALESCE(added_at, first_seen_at)) DESC);
