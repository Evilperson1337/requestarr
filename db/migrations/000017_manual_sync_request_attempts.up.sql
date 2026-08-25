BEGIN;

CREATE TABLE inventory_manual_sync_request_attempts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    request_id UUID NOT NULL,
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE RESTRICT,
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    requested_at TIMESTAMPTZ NOT NULL,
    disposition TEXT NOT NULL CHECK (disposition IN ('recorded', 'replayed', 'coalesced')),
    pending_intent_id UUID NOT NULL REFERENCES inventory_manual_sync_intents(id) ON DELETE RESTRICT
);
CREATE INDEX inventory_manual_sync_request_attempts_source_requested_idx
    ON inventory_manual_sync_request_attempts(source_id, requested_at DESC);
CREATE UNIQUE INDEX inventory_manual_sync_request_attempts_request_initial_idx
    ON inventory_manual_sync_request_attempts(request_id)
    WHERE disposition IN ('recorded', 'coalesced');
CREATE TRIGGER inventory_manual_sync_request_attempts_append_only
    BEFORE UPDATE OR DELETE ON inventory_manual_sync_request_attempts
    FOR EACH ROW EXECUTE FUNCTION prevent_inventory_source_evidence_delete();

COMMIT;
