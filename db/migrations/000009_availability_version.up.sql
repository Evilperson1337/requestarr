ALTER TABLE catalog_items
    ADD COLUMN availability_version BIGINT NOT NULL DEFAULT 0;
