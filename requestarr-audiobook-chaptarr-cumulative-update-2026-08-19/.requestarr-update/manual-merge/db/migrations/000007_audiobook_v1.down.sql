BEGIN;

-- Development-only rollback. This fails safely if audiobook rows/sources still
-- exist; remove those rows explicitly before rolling back locally.
ALTER TABLE catalog_items
    DROP CONSTRAINT catalog_items_media_type_check,
    ADD CONSTRAINT catalog_items_media_type_check
        CHECK (media_type IN ('movie','tv','album','song','game'));

ALTER TABLE library_sources
    DROP CONSTRAINT library_sources_kind_check,
    ADD CONSTRAINT library_sources_kind_check
        CHECK (kind IN ('plex','jellyfin','navidrome','romm'));

COMMIT;
