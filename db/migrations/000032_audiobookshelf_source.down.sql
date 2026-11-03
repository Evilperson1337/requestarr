DELETE FROM library_connections WHERE slug = 'audiobookshelf';
DELETE FROM library_sources WHERE kind = 'audiobookshelf';
ALTER TABLE library_connections DROP CONSTRAINT library_connections_slug;
ALTER TABLE library_connections ADD CONSTRAINT library_connections_slug CHECK (slug IN ('jellyfin','plex','romm','navidrome'));
ALTER TABLE library_sources DROP CONSTRAINT library_sources_kind_check;
ALTER TABLE library_sources ADD CONSTRAINT library_sources_kind_check CHECK (kind IN ('plex','jellyfin','navidrome','romm'));
