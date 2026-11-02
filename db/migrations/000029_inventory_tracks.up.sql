-- A track knows its album and its place in it, so an album can list its tracks (D030).
ALTER TABLE inventory_items ADD COLUMN parent_source_item_id TEXT NOT NULL DEFAULT '';
ALTER TABLE inventory_items ADD COLUMN disc INTEGER NOT NULL DEFAULT 0;
ALTER TABLE inventory_items ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
CREATE INDEX inventory_items_children ON inventory_items (source_id, parent_source_item_id) WHERE parent_source_item_id <> '';
