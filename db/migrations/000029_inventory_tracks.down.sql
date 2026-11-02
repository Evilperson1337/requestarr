DROP INDEX inventory_items_children;
ALTER TABLE inventory_items DROP COLUMN position;
ALTER TABLE inventory_items DROP COLUMN disc;
ALTER TABLE inventory_items DROP COLUMN parent_source_item_id;
