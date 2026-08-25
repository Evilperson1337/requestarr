BEGIN;

ALTER TABLE catalog_items DROP CONSTRAINT catalog_items_media_type_check;
ALTER TABLE catalog_items ADD CONSTRAINT catalog_items_media_type_check
    CHECK (media_type IN ('movie', 'tv', 'album', 'song', 'game', 'audiobook'));

ALTER TABLE library_sources DROP CONSTRAINT library_sources_kind_check;
ALTER TABLE library_sources ADD CONSTRAINT library_sources_kind_check
    CHECK (kind IN ('plex', 'jellyfin', 'navidrome', 'romm', 'audiobookshelf'));

COMMIT;
