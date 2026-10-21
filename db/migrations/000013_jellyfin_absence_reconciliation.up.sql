-- Record only successfully persisted complete snapshots. Empty snapshots are
-- ignored for negative evidence unless an operator explicitly opts in.
CREATE TABLE jellyfin_snapshot_runs (
    id BIGSERIAL PRIMARY KEY,
    observed_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    item_count BIGINT NOT NULL CHECK (item_count >= 0)
);

CREATE TABLE jellyfin_reconciliation_settings (
    source_id UUID PRIMARY KEY REFERENCES library_sources(id) ON DELETE CASCADE,
    allow_empty_snapshots BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE jellyfin_absence_state (
    catalog_item_id UUID PRIMARY KEY REFERENCES catalog_items(id) ON DELETE CASCADE,
    first_absent_at TIMESTAMPTZ NOT NULL,
    last_absent_at TIMESTAMPTZ NOT NULL,
    absence_count BIGINT NOT NULL CHECK (absence_count >= 1)
);

CREATE INDEX jellyfin_snapshot_runs_observed_idx ON jellyfin_snapshot_runs(observed_at);
