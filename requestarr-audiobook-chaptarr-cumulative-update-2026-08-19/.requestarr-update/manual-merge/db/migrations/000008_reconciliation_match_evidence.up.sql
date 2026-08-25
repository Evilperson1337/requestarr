BEGIN;

-- Preserve reconciliation provenance independently from the request status so
-- availability can be explained, audited, and safely re-evaluated as inventory
-- changes. This model is deliberately cross-media.
CREATE TABLE request_availability_matches (
    id UUID PRIMARY KEY,
    request_id UUID NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE CASCADE,
    inventory_item_id UUID REFERENCES inventory_items(id) ON DELETE SET NULL,
    match_method TEXT NOT NULL CHECK (match_method IN (
        'external_id',
        'asin_region',
        'title_author_narrator',
        'title_author',
        'title_year',
        'manual'
    )),
    confidence TEXT NOT NULL CHECK (confidence IN ('exact','high','possible')),
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    matched_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX request_availability_matches_request_active_idx
    ON request_availability_matches(request_id, active);
CREATE INDEX request_availability_matches_inventory_idx
    ON request_availability_matches(inventory_item_id)
    WHERE inventory_item_id IS NOT NULL;
CREATE INDEX request_availability_matches_review_idx
    ON request_availability_matches(confidence, active)
    WHERE confidence = 'possible' AND active = TRUE;

COMMIT;
