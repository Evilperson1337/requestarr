-- What the library says about an item (synopsis, rating, genres, runtime), shown in the Library dialog.
ALTER TABLE inventory_items ADD COLUMN details JSONB NOT NULL DEFAULT '{}'::jsonb;
