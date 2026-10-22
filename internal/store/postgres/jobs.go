package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/evilperson1337/requestarr/internal/jobs"
	"github.com/jackc/pgx/v5"
)

const abandonedRunError = "abandoned: lease expired"

// jobRunRetention is how many of a Source's newest runs are kept; older
// finished runs are pruned whenever a new run starts.
const jobRunRetention = 200

// StartRun fails runs of the Source whose lease expired (their process
// crashed), then claims the Source. The partial unique index makes the claim
// atomic, so concurrent processes cannot both win. All times are the database
// clock's, never the caller's.
func (s *Store) StartRun(ctx context.Context, sourceID, trigger string, lease time.Duration) (jobs.Run, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return jobs.Run{}, fmt.Errorf("begin job run: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE job_runs SET status='failed',finished_at=now(),error=$2 WHERE source_id=$1 AND status='running' AND lease_until<=now()`, sourceID, abandonedRunError); err != nil {
		return jobs.Run{}, fmt.Errorf("reclaim abandoned job runs: %w", err)
	}
	run := jobs.Run{SourceID: sourceID, Trigger: trigger, Status: jobs.StatusRunning}
	err = tx.QueryRow(ctx, `INSERT INTO job_runs(source_id,triggered_by,status,started_at,lease_until) VALUES($1,$2,'running',now(),now()+make_interval(secs=>$3)) ON CONFLICT(source_id) WHERE status='running' DO NOTHING RETURNING id::text,started_at`, sourceID, trigger, lease.Seconds()).Scan(&run.ID, &run.StartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return jobs.Run{}, jobs.ErrAlreadyRunning
	}
	if err != nil {
		return jobs.Run{}, fmt.Errorf("start job run: %w", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM job_runs WHERE source_id=$1 AND status<>'running' AND id NOT IN (SELECT id FROM job_runs WHERE source_id=$1 ORDER BY started_at DESC,id LIMIT $2)`, sourceID, jobRunRetention); err != nil {
		return jobs.Run{}, fmt.Errorf("prune old job runs: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return jobs.Run{}, fmt.Errorf("commit job run: %w", err)
	}
	return run, nil
}

// Heartbeat extends a running run's lease; ErrRunLost means it was reclaimed.
func (s *Store) Heartbeat(ctx context.Context, runID string, lease time.Duration) error {
	ct, err := s.pool.Exec(ctx, `UPDATE job_runs SET lease_until=now()+make_interval(secs=>$2) WHERE id=$1 AND status='running'`, runID, lease.Seconds())
	if err != nil {
		return fmt.Errorf("extend job run lease: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return jobs.ErrRunLost
	}
	return nil
}

// FinishRun records the outcome of a run still owned by the caller and
// returns the database time it finished at.
func (s *Store) FinishRun(ctx context.Context, runID string, status jobs.Status, message string) (time.Time, error) {
	var finished time.Time
	err := s.pool.QueryRow(ctx, `UPDATE job_runs SET status=$2,finished_at=now(),error=NULLIF($3,'') WHERE id=$1 AND status='running' RETURNING finished_at`, runID, status, message).Scan(&finished)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, jobs.ErrRunLost
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("finish job run: %w", err)
	}
	return finished, nil
}

// RecentRuns lists a Source's newest runs first.
func (s *Store) RecentRuns(ctx context.Context, sourceID string, limit int) ([]jobs.Run, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text,source_id::text,triggered_by,status,started_at,finished_at,COALESCE(error,'') FROM job_runs WHERE source_id=$1 ORDER BY started_at DESC,id LIMIT $2`, sourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list job runs: %w", err)
	}
	defer rows.Close()
	var runs []jobs.Run
	for rows.Next() {
		var r jobs.Run
		if err := rows.Scan(&r.ID, &r.SourceID, &r.Trigger, &r.Status, &r.StartedAt, &r.FinishedAt, &r.Error); err != nil {
			return nil, fmt.Errorf("scan job run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// RecentRunsBySource lists every Source that has runs, each with its newest runs.
func (s *Store) RecentRunsBySource(ctx context.Context, perSource int) ([]jobs.SourceRuns, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.source_id::text,COALESCE(l.name,''),r.id::text,r.triggered_by,r.status,r.started_at,r.finished_at,COALESCE(r.error,'')
		FROM (SELECT *,row_number() OVER (PARTITION BY source_id ORDER BY started_at DESC,id) AS n FROM job_runs) r
		LEFT JOIN library_sources l ON l.id=r.source_id
		WHERE r.n<=$1 ORDER BY r.source_id,r.started_at DESC,r.id`, perSource)
	if err != nil {
		return nil, fmt.Errorf("list job runs by source: %w", err)
	}
	defer rows.Close()
	var groups []jobs.SourceRuns
	for rows.Next() {
		var sourceID, name string
		var run jobs.Run
		if err := rows.Scan(&sourceID, &name, &run.ID, &run.Trigger, &run.Status, &run.StartedAt, &run.FinishedAt, &run.Error); err != nil {
			return nil, fmt.Errorf("scan job run: %w", err)
		}
		run.SourceID = sourceID
		if len(groups) == 0 || groups[len(groups)-1].SourceID != sourceID {
			groups = append(groups, jobs.SourceRuns{SourceID: sourceID, Name: name})
		}
		groups[len(groups)-1].Runs = append(groups[len(groups)-1].Runs, run)
	}
	return groups, rows.Err()
}
