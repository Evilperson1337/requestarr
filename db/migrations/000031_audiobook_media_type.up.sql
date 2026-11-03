-- Audiobooks are a media type identified by ASIN (D031).
ALTER TABLE catalog_items DROP CONSTRAINT catalog_items_media_type_check;
ALTER TABLE catalog_items ADD CONSTRAINT catalog_items_media_type_check CHECK (media_type IN ('movie','tv','album','song','game','audiobook'));
