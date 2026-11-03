-- Audiobookshelf is a Source kind (D031).
ALTER TABLE library_sources DROP CONSTRAINT library_sources_kind_check;
ALTER TABLE library_sources ADD CONSTRAINT library_sources_kind_check CHECK (kind IN ('plex','jellyfin','navidrome','romm','audiobookshelf'));
ALTER TABLE library_connections DROP CONSTRAINT library_connections_slug;
ALTER TABLE library_connections ADD CONSTRAINT library_connections_slug CHECK (slug IN ('jellyfin','plex','romm','navidrome','audiobookshelf'));
