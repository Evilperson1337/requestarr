BEGIN;

-- Complete inventory cycles are immutable audit records. They deliberately do
-- not project generic source events into inventory_items: only a future typed
-- projection may make availability decisions from this evidence.
CREATE TABLE inventory_authoritative_cycles (
    id UUID PRIMARY KEY,
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE RESTRICT,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL,
    CHECK (completed_at >= started_at)
);
CREATE INDEX inventory_authoritative_cycles_source_completed_idx
    ON inventory_authoritative_cycles(source_id, completed_at DESC);

CREATE TABLE inventory_authoritative_cycle_events (
    cycle_id UUID NOT NULL REFERENCES inventory_authoritative_cycles(id) ON DELETE RESTRICT,
    source_item_id TEXT NOT NULL CHECK (octet_length(source_item_id) BETWEEN 1 AND 256),
    change_kind TEXT NOT NULL CHECK (change_kind IN ('added', 'updated', 'removed')),
    media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'tv', 'album', 'song', 'game', 'audiobook')),
    PRIMARY KEY (cycle_id, source_item_id)
);

-- The current state is a small operational projection. Every mutation also has
-- immutable transition evidence below, keyed by the caller's durable cycle ID.
CREATE TABLE inventory_source_status (
    source_id UUID PRIMARY KEY REFERENCES library_sources(id) ON DELETE RESTRICT,
    state TEXT NOT NULL CHECK (state IN ('idle', 'running', 'succeeded', 'failed')),
    last_started_at TIMESTAMPTZ,
    last_completed_at TIMESTAMPTZ,
    last_successful_at TIMESTAMPTZ,
    failure_detail TEXT NOT NULL DEFAULT '' CHECK (failure_detail IN ('', 'authentication failed', 'request timed out', 'source unavailable', 'invalid source response')),
    CHECK (last_completed_at IS NULL OR last_started_at IS NULL OR last_completed_at >= last_started_at),
    CHECK (last_successful_at IS NULL OR last_completed_at IS NULL OR last_successful_at <= last_completed_at),
    CHECK ((state = 'failed') = (failure_detail <> ''))
);

CREATE TABLE inventory_source_status_transitions (
    source_id UUID NOT NULL REFERENCES library_sources(id) ON DELETE RESTRICT,
    cycle_id UUID NOT NULL,
    from_state TEXT NOT NULL CHECK (from_state IN ('idle', 'running', 'succeeded', 'failed')),
    to_state TEXT NOT NULL CHECK (to_state IN ('idle', 'running', 'succeeded', 'failed')),
    occurred_at TIMESTAMPTZ NOT NULL,
    failure_detail TEXT NOT NULL DEFAULT '' CHECK (failure_detail IN ('', 'authentication failed', 'request timed out', 'source unavailable', 'invalid source response')),
    applied_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (source_id, cycle_id),
    CHECK ((to_state = 'failed') = (failure_detail <> ''))
);
CREATE INDEX inventory_source_status_transitions_source_applied_idx
    ON inventory_source_status_transitions(source_id, applied_at DESC);

-- Job payloads are opaque and bounded. Queue state is durable; each outcome is
-- recorded independently so operational history survives row-state changes.
CREATE TABLE durable_jobs (
    id UUID PRIMARY KEY,
    idempotency_key UUID NOT NULL UNIQUE,
    kind TEXT NOT NULL CHECK (octet_length(kind) BETWEEN 1 AND 128),
    payload BYTEA NOT NULL CHECK (octet_length(payload) <= 65536),
    attempt INTEGER NOT NULL CHECK (attempt BETWEEN 1 AND 100),
    state TEXT NOT NULL CHECK (state IN ('queued', 'leased', 'completed', 'failed')),
    available_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    leased_at TIMESTAMPTZ,
    lease_token UUID,
    lease_expires_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    failure_code TEXT CHECK (failure_code IS NULL OR octet_length(failure_code) BETWEEN 1 AND 64),
    failure_message TEXT CHECK (failure_message IS NULL OR octet_length(failure_message) BETWEEN 1 AND 512),
    CHECK ((state = 'leased') = (lease_token IS NOT NULL AND lease_expires_at IS NOT NULL AND leased_at IS NOT NULL)),
    CHECK (lease_expires_at IS NULL OR (lease_expires_at > leased_at AND lease_expires_at <= leased_at + INTERVAL '1 hour')),
    CHECK ((failure_code IS NULL) = (failure_message IS NULL)),
    CHECK ((state = 'completed') = (completed_at IS NOT NULL)),
    CHECK ((state = 'failed') = (failed_at IS NOT NULL))
);
CREATE INDEX durable_jobs_claim_due_idx
    ON durable_jobs(available_at, created_at)
    WHERE state = 'queued';
CREATE INDEX durable_jobs_expired_lease_idx
    ON durable_jobs(lease_expires_at)
    WHERE state = 'leased';

CREATE TABLE durable_job_events (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES durable_jobs(id) ON DELETE RESTRICT,
    event_type TEXT NOT NULL CHECK (event_type IN ('enqueued', 'claimed', 'completed', 'retried', 'failed')),
    attempt INTEGER NOT NULL CHECK (attempt BETWEEN 1 AND 100),
    failure_code TEXT CHECK (failure_code IS NULL OR octet_length(failure_code) BETWEEN 1 AND 64),
    failure_message TEXT CHECK (failure_message IS NULL OR octet_length(failure_message) BETWEEN 1 AND 512),
    created_at TIMESTAMPTZ NOT NULL,
    CHECK ((failure_code IS NULL) = (failure_message IS NULL))
);
CREATE INDEX durable_job_events_job_created_idx
    ON durable_job_events(job_id, created_at DESC);

COMMIT;
