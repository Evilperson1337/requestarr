-- Small key/value store for instance-wide policy settings (D035). Values are
-- plain text and never secrets; secrets live in the encrypted secrets table.
CREATE TABLE app_settings (
    key TEXT PRIMARY KEY CHECK (length(key) BETWEEN 1 AND 128),
    value TEXT NOT NULL CHECK (length(value) <= 4096),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A notification may point at the place the reader should go next (an
-- application path such as /settings?tab=users).
ALTER TABLE notification_deliveries
    ADD COLUMN link TEXT CHECK (link IS NULL OR (length(link) <= 512 AND left(link, 1) = '/' AND left(link, 2) <> '//'));
