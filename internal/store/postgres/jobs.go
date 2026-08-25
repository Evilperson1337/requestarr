package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"git.example.com/evilperson1337/Requestarr/internal/jobs"
)

var (
	ErrJobPersistenceUnavailable = errors.New("durable job persistence unavailable")
	ErrJobPersistenceInvalid     = errors.New("durable job persistence input is invalid")
	ErrJobLeaseLost              = errors.New("durable job lease is not current")
)

// JobRepository is the PostgreSQL-backed durable queue. PostgreSQL is the sole
// time authority for due-time, lease-expiry, retry scheduling, and outcomes.
type JobRepository struct{ pool *Pool }

func NewJobRepository(pool *Pool) *JobRepository { return &JobRepository{pool: pool} }

func (r *JobRepository) Enqueue(ctx context.Context, request jobs.EnqueueRequest) (jobs.EnqueueResult, error) {
	if err := jobs.ValidateEnqueueRequest(request); err != nil {
		return jobs.EnqueueResult{}, fmt.Errorf("%w: %v", ErrJobPersistenceInvalid, err)
	}
	if err := r.available(ctx); err != nil {
		return jobs.EnqueueResult{}, err
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return jobs.EnqueueResult{}, classifyJobError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var existing jobs.DurableJob
	id, err := newUUID()
	if err != nil {
		return jobs.EnqueueResult{}, ErrJobPersistenceUnavailable
	}
	eventID, err := newUUID()
	if err != nil {
		return jobs.EnqueueResult{}, ErrJobPersistenceUnavailable
	}
	err = tx.QueryRow(ctx, `INSERT INTO durable_jobs(id,idempotency_key,kind,payload,attempt,state,available_at,created_at) VALUES($1,$2,$3,$4,1,'queued',clock_timestamp(),clock_timestamp()) ON CONFLICT (idempotency_key) DO NOTHING RETURNING id,kind,payload,attempt,available_at,created_at`, id, request.IdempotencyKey, request.Kind, request.Payload).Scan(&existing.ID, &existing.Kind, &existing.Payload, &existing.Attempt, &existing.AvailableAt, &existing.CreatedAt)
	created := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT id,kind,payload,attempt,available_at,created_at FROM durable_jobs WHERE idempotency_key=$1`, request.IdempotencyKey).Scan(&existing.ID, &existing.Kind, &existing.Payload, &existing.Attempt, &existing.AvailableAt, &existing.CreatedAt)
	}
	if err != nil {
		return jobs.EnqueueResult{}, classifyJobError(ctx, err)
	}
	if existing.Kind != request.Kind || !bytes.Equal(existing.Payload, request.Payload) {
		return jobs.EnqueueResult{}, ErrJobPersistenceInvalid
	}
	if created {
		if _, err = tx.Exec(ctx, `INSERT INTO durable_job_events(id,job_id,event_type,attempt,created_at) VALUES($1,$2,'enqueued',1,clock_timestamp())`, eventID, id); err != nil {
			return jobs.EnqueueResult{}, classifyJobError(ctx, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return jobs.EnqueueResult{}, classifyJobError(ctx, err)
	}
	return jobs.EnqueueResult{Job: existing, Created: created}, nil
}

func (r *JobRepository) ClaimDue(ctx context.Context, leaseDuration time.Duration, kinds []string) (*jobs.LeasedJob, error) {
	if err := jobs.ValidateLeaseDuration(leaseDuration); err != nil || len(kinds) == 0 {
		return nil, ErrJobPersistenceInvalid
	}
	if err := r.available(ctx); err != nil {
		return nil, err
	}
	token, err := newUUID()
	if err != nil {
		return nil, ErrJobPersistenceUnavailable
	}
	eventID, err := newUUID()
	if err != nil {
		return nil, ErrJobPersistenceUnavailable
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, classifyJobError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// A lease expiry is an execution failure. Consume an attempt before
	// re-leasing, and terminally fail exhausted jobs so they cannot be reclaimed
	// forever. This also causes a stale worker to lose its old token.
	var exhaustedID string
	err = tx.QueryRow(ctx, `WITH exhausted AS (
 SELECT id FROM durable_jobs WHERE state='leased' AND kind = ANY($1::text[]) AND lease_expires_at <= clock_timestamp() AND attempt >= 100
 ORDER BY lease_expires_at FOR UPDATE SKIP LOCKED LIMIT 1
	 ) UPDATE durable_jobs j SET state='failed',failed_at=clock_timestamp(),lease_token=NULL,lease_expires_at=NULL,leased_at=NULL,failure_code='lease_expired',failure_message=NULL
 FROM exhausted WHERE j.id=exhausted.id RETURNING j.id`, kinds).Scan(&exhaustedID)
	if err == nil {
		if _, err = tx.Exec(ctx, `INSERT INTO durable_job_events(id,job_id,event_type,attempt,failure_code,created_at) VALUES($1,$2,'failed',100,'lease_expired',clock_timestamp())`, eventID, exhaustedID); err != nil {
			return nil, classifyJobError(ctx, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, classifyJobError(ctx, err)
		}
		return nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, classifyJobError(ctx, err)
	}
	var claimed jobs.LeasedJob
	err = tx.QueryRow(ctx, `WITH candidate AS (
 SELECT id FROM durable_jobs
 WHERE kind = ANY($3::text[]) AND ((state='queued' AND available_at <= clock_timestamp())
    OR (state='leased' AND lease_expires_at <= clock_timestamp() AND attempt < 100))
 ORDER BY available_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1
), updated AS (
 UPDATE durable_jobs j SET state='leased',attempt=CASE WHEN j.state='leased' THEN j.attempt+1 ELSE j.attempt END,leased_at=clock_timestamp(),lease_token=$1,lease_expires_at=clock_timestamp()+$2::interval
 FROM candidate WHERE j.id=candidate.id
 RETURNING j.id,j.kind,j.payload,j.attempt,j.available_at,j.created_at,j.lease_token,j.lease_expires_at
 ) SELECT * FROM updated`, token, leaseDuration.String(), kinds).Scan(&claimed.Job.ID, &claimed.Job.Kind, &claimed.Job.Payload, &claimed.Job.Attempt, &claimed.Job.AvailableAt, &claimed.Job.CreatedAt, &claimed.Lease.Token, &claimed.Lease.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return nil, classifyJobError(ctx, err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, classifyJobError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO durable_job_events(id,job_id,event_type,attempt,created_at) VALUES($1,$2,'claimed',$3,clock_timestamp())`, eventID, claimed.Job.ID, claimed.Job.Attempt); err != nil {
		return nil, classifyJobError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, classifyJobError(ctx, err)
	}
	return &claimed, nil
}

func (r *JobRepository) Complete(ctx context.Context, jobID, leaseToken string) error {
	if !validJobID(jobID) || !validJobID(leaseToken) {
		return ErrJobPersistenceInvalid
	}
	return r.outcome(ctx, jobID, leaseToken, "completed", 0, jobs.FailureData{})
}

func (r *JobRepository) Retry(ctx context.Context, outcome jobs.RetryOutcome) error {
	if err := jobs.ValidateRetryOutcome(outcome); err != nil {
		return fmt.Errorf("%w: %v", ErrJobPersistenceInvalid, err)
	}
	return r.outcome(ctx, outcome.JobID, outcome.LeaseToken, "retried", outcome.Delay, outcome.Failure)
}

func (r *JobRepository) Fail(ctx context.Context, outcome jobs.FailureOutcome) error {
	if err := jobs.ValidateFailureOutcome(outcome); err != nil {
		return fmt.Errorf("%w: %v", ErrJobPersistenceInvalid, err)
	}
	return r.outcome(ctx, outcome.JobID, outcome.LeaseToken, "failed", 0, outcome.Failure)
}

func (r *JobRepository) outcome(ctx context.Context, jobID, leaseToken, eventType string, delay time.Duration, failure jobs.FailureData) error {
	if err := r.available(ctx); err != nil {
		return err
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return classifyJobError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	eventID, err := newUUID()
	if err != nil {
		return ErrJobPersistenceUnavailable
	}
	var attempt int
	var query string
	var args []any
	switch eventType {
	case "completed":
		query = `UPDATE durable_jobs SET state='completed',completed_at=clock_timestamp(),lease_token=NULL,lease_expires_at=NULL,leased_at=NULL,failure_code=NULL,failure_message=NULL WHERE id=$1 AND state='leased' AND lease_token=$2 AND lease_expires_at > clock_timestamp() RETURNING attempt`
		args = []any{jobID, leaseToken}
	case "retried":
		query = `UPDATE durable_jobs SET state='queued',attempt=attempt+1,available_at=clock_timestamp()+$3::interval,lease_token=NULL,lease_expires_at=NULL,leased_at=NULL,failure_code=$4,failure_message=$5 WHERE id=$1 AND state='leased' AND lease_token=$2 AND lease_expires_at > clock_timestamp() AND attempt < 100 RETURNING attempt`
		args = []any{jobID, leaseToken, delay.String(), failure.Code, nil}
	case "failed":
		query = `UPDATE durable_jobs SET state='failed',failed_at=clock_timestamp(),lease_token=NULL,lease_expires_at=NULL,leased_at=NULL,failure_code=$3,failure_message=$4 WHERE id=$1 AND state='leased' AND lease_token=$2 AND lease_expires_at > clock_timestamp() RETURNING attempt`
		args = []any{jobID, leaseToken, failure.Code, nil}
	}
	err = tx.QueryRow(ctx, query, args...).Scan(&attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrJobLeaseLost
	}
	if err != nil {
		return classifyJobError(ctx, err)
	}
	if eventType == "retried" {
		attempt--
	}
	if _, err = tx.Exec(ctx, `INSERT INTO durable_job_events(id,job_id,event_type,attempt,failure_code,created_at) VALUES($1,$2,$3,$4,$5,clock_timestamp())`, eventID, jobID, eventType, attempt, nullIfEmpty(string(failure.Code))); err != nil {
		return classifyJobError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return classifyJobError(ctx, err)
	}
	return nil
}

func (r *JobRepository) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrJobPersistenceUnavailable, err)
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return ErrJobPersistenceUnavailable
	}
	return nil
}

func classifyJobError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %v", ErrJobPersistenceUnavailable, ctx.Err())
	}
	return ErrJobPersistenceUnavailable
}

var canonicalUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validJobID(value string) bool { return canonicalUUIDPattern.MatchString(value) }

var _ jobs.Repository = (*JobRepository)(nil)
var _ jobs.KnownKindRepository = (*JobRepository)(nil)
