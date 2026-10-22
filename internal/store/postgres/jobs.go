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

// StartRun fails runs of the Source whose lease expired (their process
// crashed), then claims the Source. The partial unique index makes the claim
// atomic, so concurrent processes cannot both win.
func (s *Store) StartRun(ctx context.Context, sourceID, trigger string, lease time.Duration, now time.Time) (jobs.Run, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return jobs.Run{}, fmt.Errorf("begin job run: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE job_runs SET status='failed',finished_at=$2,error=$3 WHERE source_id=$1 AND status='running' AND lease_until<=$2`, sourceID, now, abandonedRunError); err != nil {
		return jobs.Run{}, fmt.Errorf("reclaim abandoned job runs: %w", err)
	}
	run := jobs.Run{SourceID: sourceID, Trigger: trigger, Status: jobs.StatusRunning, StartedAt: now}
	err = tx.QueryRow(ctx, `INSERT INTO job_runs(source_id,triggered_by,status,started_at,lease_until) VALUES($1,$2,'running',$3,$4) ON CONFLICT(source_id) WHERE status='running' DO NOTHING RETURNING id::text`, sourceID, trigger, now, now.Add(lease)).Scan(&run.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return jobs.Run{}, jobs.ErrAlreadyRunning
	}
	if err != nil {
		return jobs.Run{}, fmt.Errorf("start job run: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return jobs.Run{}, fmt.Errorf("commit job run: %w", err)
	}
	return run, nil
}

// Heartbeat extends a running run's lease; ErrRunLost means it was reclaimed.
func (s *Store) Heartbeat(ctx context.Context, runID string, lease time.Duration, now time.Time) error {
	ct, err := s.pool.Exec(ctx, `UPDATE job_runs SET lease_until=$2 WHERE id=$1 AND status='running'`, runID, now.Add(lease))
	if err != nil {
		return fmt.Errorf("extend job run lease: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return jobs.ErrRunLost
	}
	return nil
}

// FinishRun records the outcome of a run still owned by the caller.
func (s *Store) FinishRun(ctx context.Context, runID string, status jobs.Status, message string, now time.Time) error {
	ct, err := s.pool.Exec(ctx, `UPDATE job_runs SET status=$2,finished_at=$3,error=NULLIF($4,'') WHERE id=$1 AND status='running'`, runID, status, now, message)
	if err != nil {
		return fmt.Errorf("finish job run: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return jobs.ErrRunLost
	}
	return nil
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
