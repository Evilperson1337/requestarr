BEGIN;

-- Applied migrations are immutable. Extend the existing V1 constraints rather
-- than editing 000001_initial.
ALTER TABLE catalog_items
    DROP CONSTRAINT catalog_items_media_type_check,
    ADD CONSTRAINT catalog_items_media_type_check
        CHECK (media_type IN ('movie','tv','album','song','game','audiobook'));

ALTER TABLE library_sources
    DROP CONSTRAINT library_sources_kind_check,
    ADD CONSTRAINT library_sources_kind_check
        CHECK (kind IN ('plex','jellyfin','navidrome','romm','audiobookshelf'));

-- Exact external IDs (notably ASIN) continue to live in inventory_items.external_ids
-- so the existing GIN index can support targeted reconciliation without a new
-- audiobook-specific table. Provider/edition details remain in raw_metadata.

COMMIT;
