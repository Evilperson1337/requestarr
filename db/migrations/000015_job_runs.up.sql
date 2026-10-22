-- Background job runs (D018). One row per run; the partial unique index is the
-- no-overlap guard and lives in PostgreSQL so it survives process restarts.
-- A running row whose lease_until has passed belongs to a crashed process and
-- is failed by the next StartRun for that Source.
CREATE TABLE job_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id UUID NOT NULL,
    triggered_by TEXT NOT NULL CHECK (triggered_by IN ('schedule', 'manual')),
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    started_at TIMESTAMPTZ NOT NULL,
    lease_until TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    error TEXT CHECK (error IS NULL OR length(error) <= 256),
    CHECK ((status = 'running') = (finished_at IS NULL))
);

CREATE UNIQUE INDEX job_runs_one_running_per_source_idx ON job_runs(source_id) WHERE status = 'running';
CREATE INDEX job_runs_source_started_idx ON job_runs(source_id, started_at DESC);
