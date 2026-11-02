-- A short note a library knows about an item, such as a game's platform name (D029).
ALTER TABLE inventory_items ADD COLUMN detail TEXT NOT NULL DEFAULT '';
