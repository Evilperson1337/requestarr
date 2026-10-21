CREATE TABLE catalog_item_platforms (
    catalog_item_id UUID NOT NULL REFERENCES catalog_items(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL,
    platform_key TEXT NOT NULL,
    name TEXT NOT NULL,
    PRIMARY KEY (catalog_item_id, platform_key),
    UNIQUE (catalog_item_id, provider_id)
);
