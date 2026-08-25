BEGIN;

-- Evidence writes need an append-only audit trail. This table intentionally
-- carries no payload or metadata: the referenced bounded ledger row is proof.
CREATE TABLE reconciliation_match_events (
    id UUID PRIMARY KEY,
    evidence_id UUID NOT NULL REFERENCES reconciliation_match_evidence(id) ON DELETE RESTRICT,
    event_type TEXT NOT NULL CHECK (event_type IN ('activated', 'deactivated')),
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX reconciliation_match_events_evidence_created_idx
    ON reconciliation_match_events(evidence_id, created_at DESC);

COMMIT;
