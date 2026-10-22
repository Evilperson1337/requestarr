-- The same immutable normalization on both sides keeps the candidate lookup
-- indexed. Punctuation and whitespace separate words; empty titles never match.
CREATE FUNCTION requestarr_match_title(TEXT) RETURNS TEXT
LANGUAGE SQL IMMUTABLE STRICT PARALLEL SAFE
AS $$ SELECT btrim(regexp_replace(lower($1), '[^[:alnum:]]+', ' ', 'g')) $$;

CREATE INDEX catalog_match_title_idx ON catalog_items(media_type, requestarr_match_title(title));

CREATE TABLE match_suggestions (
    id UUID PRIMARY KEY,
    catalog_item_id UUID NOT NULL REFERENCES catalog_items(id) ON DELETE CASCADE,
    inventory_item_id UUID NOT NULL REFERENCES inventory_items(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','dismissed')),
    confidence TEXT NOT NULL CHECK (confidence IN ('title_year','title_only')),
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    UNIQUE(catalog_item_id, inventory_item_id)
);
CREATE INDEX match_suggestions_pending_idx ON match_suggestions(created_at, id) WHERE status='pending';

CREATE TABLE match_suggestion_events (
    id UUID PRIMARY KEY,
    suggestion_id UUID NOT NULL REFERENCES match_suggestions(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL CHECK (event_type IN ('match.suggested','match.accepted','match.dismissed')),
    actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE(suggestion_id, event_type)
);
