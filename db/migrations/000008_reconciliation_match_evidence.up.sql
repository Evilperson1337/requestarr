BEGIN;

-- This ledger records bounded, normalized reconciliation provenance only. It
-- deliberately stores neither provider payloads nor secrets, and it does not
-- change request status or availability.
ALTER TABLE inventory_items
    ADD CONSTRAINT inventory_items_id_source_id_key UNIQUE (id, source_id);

CREATE TABLE reconciliation_match_evidence (
    id UUID PRIMARY KEY,
    request_id UUID NOT NULL REFERENCES requests(id) ON DELETE RESTRICT,
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE RESTRICT,
    inventory_item_id UUID NOT NULL,
    method TEXT NOT NULL CHECK (method IN (
        'external_id',
        'asin_region',
        'title_author_narrator',
        'title_author',
        'title_year',
        'manual'
    )),
    confidence TEXT NOT NULL CHECK (confidence IN ('exact', 'high', 'possible')),
    reason TEXT NOT NULL DEFAULT '' CHECK (octet_length(reason) <= 512),
    evidence JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (
        jsonb_typeof(evidence) = 'array'
        AND jsonb_array_length(evidence) <= 16
        AND octet_length(evidence::text) <= 8192
        AND NOT jsonb_path_exists(evidence,
            '$[*] ? (@.type() != "object" || @.size() != 2 || !exists(@.key) || !exists(@.value) || @.key.type() != "string" || @.value.type() != "string" || @.key like_regex "^.{49,}$" flag "s" || @.value like_regex "^.{257,}$" flag "s")'
        )
    ),
    review_state TEXT NOT NULL DEFAULT 'not_required' CHECK (
        review_state IN ('not_required', 'pending', 'accepted', 'rejected')
    ),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deactivated_at TIMESTAMPTZ,
    FOREIGN KEY (inventory_item_id, source_id)
        REFERENCES inventory_items(id, source_id) ON DELETE RESTRICT,
    CHECK (
        (confidence = 'possible' AND review_state IN ('pending', 'accepted', 'rejected'))
        OR (confidence IN ('exact', 'high') AND review_state = 'not_required')
    ),
    CHECK (
        (active AND deactivated_at IS NULL)
        OR (NOT active AND deactivated_at IS NOT NULL AND deactivated_at >= created_at)
    )
);

-- A partial unique index permits a new active association after its prior
-- evidence has been deactivated, while making active association writes
-- idempotent for a request and inventory item.
CREATE UNIQUE INDEX reconciliation_match_evidence_active_association_key
    ON reconciliation_match_evidence(request_id, inventory_item_id)
    WHERE active;
CREATE INDEX reconciliation_match_evidence_request_active_idx
    ON reconciliation_match_evidence(request_id, observed_at DESC)
    WHERE active;
CREATE INDEX reconciliation_match_evidence_inventory_active_idx
    ON reconciliation_match_evidence(inventory_item_id, observed_at DESC)
    WHERE active;
CREATE INDEX reconciliation_match_evidence_possible_review_idx
    ON reconciliation_match_evidence(observed_at ASC)
    WHERE active AND review_state = 'pending';

COMMIT;
