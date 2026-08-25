//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/jobs"
	"git.example.com/evilperson1337/Requestarr/internal/store/postgres/migrations"
)

func TestJobRepositoryLifecycleLeaseAndIdempotency(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	repo := NewJobRepository(&Pool{pool: pool})
	request := jobs.EnqueueRequest{Kind: "inventory.sync", Payload: []byte(`{"source":"safe"}`), IdempotencyKey: "00000000-0000-4000-8000-000000000201"}
	queued, err := repo.Enqueue(ctx, request)
	if err != nil || !queued.Created || queued.Job.Attempt != 1 {
		t.Fatalf("enqueue=%+v err=%v", queued, err)
	}
	replay, err := repo.Enqueue(ctx, request)
	if err != nil || replay.Created || replay.Job.ID != queued.Job.ID {
		t.Fatalf("enqueue replay=%+v err=%v", replay, err)
	}
	leased, err := repo.ClaimDue(ctx, time.Minute, []string{"inventory.sync"})
	if err != nil || leased == nil || leased.Lease.ExpiresAt.Before(leased.Job.CreatedAt) {
		t.Fatalf("claim=%+v err=%v", leased, err)
	}
	if err = repo.Complete(ctx, leased.Job.ID, "00000000-0000-4000-8000-000000000202"); !errors.Is(err, ErrJobLeaseLost) {
		t.Fatalf("wrong lease completion=%v", err)
	}
	if err = repo.Retry(ctx, jobs.RetryOutcome{JobID: leased.Job.ID, LeaseToken: leased.Lease.Token, Delay: time.Second, Failure: jobs.FailureData{Code: jobs.FailureCodeHandlerFailed}}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimDue(ctx, time.Minute, []string{"inventory.sync"}); err != nil || claimed != nil {
		t.Fatalf("claim before DB retry time=%+v err=%v", claimed, err)
	}
	time.Sleep(1100 * time.Millisecond)
	leased, err = repo.ClaimDue(ctx, time.Minute, []string{"inventory.sync"})
	if err != nil || leased == nil || leased.Job.Attempt != 2 {
		t.Fatalf("retry claim=%+v err=%v", leased, err)
	}
	if err = repo.Fail(ctx, jobs.FailureOutcome{JobID: leased.Job.ID, LeaseToken: leased.Lease.Token, Failure: jobs.FailureData{Code: jobs.FailureCodeHandlerFailed}}); err != nil {
		t.Fatal(err)
	}
	var state, code string
	var message *string
	if err = pool.QueryRow(ctx, `SELECT state,failure_code,failure_message FROM durable_jobs WHERE id=$1`, leased.Job.ID).Scan(&state, &code, &message); err != nil || state != "failed" || code != string(jobs.FailureCodeHandlerFailed) || message != nil {
		t.Fatalf("job state=%q code=%q message=%v err=%v", state, code, message, err)
	}
	unknown, err := repo.Enqueue(ctx, jobs.EnqueueRequest{Kind: "future.sync", Payload: []byte(`{}`), IdempotencyKey: "00000000-0000-4000-8000-000000000203"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimDue(ctx, time.Minute, []string{"inventory.sync"}); err != nil || claimed != nil {
		t.Fatalf("known-kind claim consumed unknown work: claim=%+v err=%v", claimed, err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM durable_jobs WHERE id=$1`, unknown.Job.ID).Scan(&state); err != nil || state != "queued" {
		t.Fatalf("unknown job state=%q err=%v", state, err)
	}
}

func TestJobRepositoryConcurrentEnqueueAndExpiredLeaseRecovery(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	repo := NewJobRepository(&Pool{pool: pool})
	request := jobs.EnqueueRequest{Kind: "inventory.sync", Payload: []byte(`{"source":"safe"}`), IdempotencyKey: "00000000-0000-4000-8000-000000000211"}
	const callers = 16
	results := make(chan jobs.EnqueueResult, callers)
	errs := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() { defer group.Done(); result, err := repo.Enqueue(ctx, request); results <- result; errs <- err }()
	}
	group.Wait()
	close(results)
	close(errs)
	created, jobID := 0, ""
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range results {
		if result.Created {
			created++
		}
		if jobID == "" {
			jobID = result.Job.ID
		}
		if result.Job.ID != jobID {
			t.Fatalf("idempotent callers produced jobs %q and %q", jobID, result.Job.ID)
		}
	}
	if created != 1 {
		t.Fatalf("created=%d, want one", created)
	}
	first, err := repo.ClaimDue(ctx, time.Second, []string{"inventory.sync"})
	if err != nil || first == nil {
		t.Fatalf("first claim=%+v err=%v", first, err)
	}
	time.Sleep(1100 * time.Millisecond)
	second, err := repo.ClaimDue(ctx, time.Minute, []string{"inventory.sync"})
	if err != nil || second == nil || second.Job.Attempt != 2 {
		t.Fatalf("reclaimed=%+v err=%v", second, err)
	}
	if err := repo.Complete(ctx, first.Job.ID, first.Lease.Token); !errors.Is(err, ErrJobLeaseLost) {
		t.Fatalf("stale completion error=%v", err)
	}
	if err := repo.Complete(ctx, second.Job.ID, second.Lease.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimDue(ctx, jobs.MaxLeaseDuration+time.Nanosecond, []string{"inventory.sync"}); !errors.Is(err, ErrJobPersistenceInvalid) {
		t.Fatalf("unbounded lease error=%v", err)
	}
	if err := repo.Complete(ctx, second.Job.ID, "00000000-0000-4000-8000-0000000002AB"); !errors.Is(err, ErrJobPersistenceInvalid) {
		t.Fatalf("noncanonical token error=%v", err)
	}
	exhaustedID := "00000000-0000-4000-8000-000000000212"
	if _, err := pool.Exec(ctx, `INSERT INTO durable_jobs(id,idempotency_key,kind,payload,attempt,state,available_at,created_at,leased_at,lease_token,lease_expires_at) VALUES($1,'00000000-0000-4000-8000-000000000213','inventory.sync','',100,'leased',clock_timestamp(),clock_timestamp()-interval '2 hours',clock_timestamp()-interval '2 hours','00000000-0000-4000-8000-000000000214',clock_timestamp()-interval '1 hour')`, exhaustedID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimDue(ctx, time.Minute, []string{"inventory.sync"}); err != nil || claimed != nil {
		t.Fatalf("exhausted recovery claim=%+v err=%v", claimed, err)
	}
	var state, failureCode string
	if err := pool.QueryRow(ctx, `SELECT state,failure_code FROM durable_jobs WHERE id=$1`, exhaustedID).Scan(&state, &failureCode); err != nil || state != "failed" || failureCode != "lease_expired" {
		t.Fatalf("exhausted state=%q code=%q err=%v", state, failureCode, err)
	}
}

func TestJobRepositoryRejectsUnsafeFailureDiagnostic(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	repo := NewJobRepository(&Pool{pool: pool})
	if err := repo.Fail(ctx, jobs.FailureOutcome{
		JobID:      "00000000-0000-4000-8000-000000000221",
		LeaseToken: "00000000-0000-4000-8000-000000000222",
		Failure:    jobs.FailureData{Code: jobs.FailureCode("GET https://provider.example.test/?access_token=secret")},
	}); !errors.Is(err, ErrJobPersistenceInvalid) {
		t.Fatalf("Fail() unsafe diagnostic error = %v", err)
	}
}
