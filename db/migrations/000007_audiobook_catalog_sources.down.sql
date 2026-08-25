BEGIN;

-- This local/test rollback is intentionally data-safe: historical audiobook
-- catalog items or Audiobookshelf sources must be removed or migrated before
-- the former constraints can be restored.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM catalog_items WHERE media_type = 'audiobook') THEN
        RAISE EXCEPTION 'cannot roll back migration 000007 while audiobook catalog items exist';
    END IF;
    IF EXISTS (SELECT 1 FROM library_sources WHERE kind = 'audiobookshelf') THEN
        RAISE EXCEPTION 'cannot roll back migration 000007 while Audiobookshelf sources exist';
    END IF;
END $$;

ALTER TABLE catalog_items DROP CONSTRAINT catalog_items_media_type_check;
ALTER TABLE catalog_items ADD CONSTRAINT catalog_items_media_type_check
    CHECK (media_type IN ('movie', 'tv', 'album', 'song', 'game'));

ALTER TABLE library_sources DROP CONSTRAINT library_sources_kind_check;
ALTER TABLE library_sources ADD CONSTRAINT library_sources_kind_check
    CHECK (kind IN ('plex', 'jellyfin', 'navidrome', 'romm'));

COMMIT;
