-- What part of the item a Request covers (D025). '{}' is the whole item, so
-- every existing Request keeps its meaning.
ALTER TABLE requests ADD COLUMN selection JSONB NOT NULL DEFAULT '{}'::jsonb;
