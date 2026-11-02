-- Where each library Source lives (D028). Credentials stay in stored_secrets;
-- an environment URL still wins over a row here.
CREATE TABLE library_connections (
    slug       TEXT PRIMARY KEY,
    url        TEXT NOT NULL,
    username   TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT library_connections_slug CHECK (slug IN ('jellyfin','plex','romm','navidrome')),
    CONSTRAINT library_connections_url CHECK (url <> '' AND length(url) <= 2048)
);
