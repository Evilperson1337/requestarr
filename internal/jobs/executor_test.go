package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

type repositoryFake struct {
	claimed            *LeasedJob
	kinds              []string
	complete           bool
	completeContextErr error
	retry              RetryOutcome
	failure            FailureOutcome
	failed             bool
}

func (f *repositoryFake) Enqueue(context.Context, EnqueueRequest) (EnqueueResult, error) {
	return EnqueueResult{}, nil
}
func (f *repositoryFake) ClaimDue(_ context.Context, _ time.Duration, kinds []string) (*LeasedJob, error) {
	f.kinds = append([]string(nil), kinds...)
	return f.claimed, nil
}
func (f *repositoryFake) Complete(ctx context.Context, _ string, _ string) error {
	f.complete = true
	f.completeContextErr = ctx.Err()
	return nil
}
func (f *repositoryFake) Retry(_ context.Context, outcome RetryOutcome) error {
	f.retry = outcome
	return nil
}
func (f *repositoryFake) Fail(_ context.Context, outcome FailureOutcome) error {
	f.failed = true
	f.failure = outcome
	return nil
}

type handlerFake struct {
	kind string
	err  error
}

func (f handlerFake) Kind() string                             { return f.kind }
func (f handlerFake) Handle(context.Context, DurableJob) error { return f.err }

type deadlineHandler struct {
	deadline chan<- struct{}
}

func (deadlineHandler) Kind() string { return "sync" }

func (h deadlineHandler) Handle(ctx context.Context, _ DurableJob) error {
	h.deadline <- struct{}{}
	<-ctx.Done()
	return ctx.Err()
}

func TestRetryPolicyDelay(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 4, InitialDelay: time.Second, MaxDelay: 3 * time.Second}
	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{{1, time.Second}, {2, 2 * time.Second}, {3, 3 * time.Second}} {
		got, err := policy.Delay(tc.attempt)
		if err != nil || got != tc.want {
			t.Fatalf("Delay(%d) = %v, %v", tc.attempt, got, err)
		}
	}
}

func TestExecutorRunOnce(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	job := &LeasedJob{Job: DurableJob{ID: jobID, Kind: "sync", Attempt: 1, AvailableAt: now, CreatedAt: now}, Lease: Lease{Token: leaseID, ExpiresAt: now.Add(time.Minute)}}
	for _, tc := range []struct {
		name                     string
		handler                  handlerFake
		wantComplete, wantFailed bool
		wantRetry                bool
	}{
		{name: "complete", handler: handlerFake{kind: "sync"}, wantComplete: true},
		{name: "retry", handler: handlerFake{kind: "sync", err: errors.New("temporary")}, wantRetry: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &repositoryFake{claimed: job}
			executor, err := NewExecutor(repository, []Handler{tc.handler}, RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			ran, err := executor.RunOnce(context.Background())
			if err != nil || !ran {
				t.Fatalf("RunOnce() = %v, %v", ran, err)
			}
			if repository.complete != tc.wantComplete || repository.failed != tc.wantFailed || (tc.wantRetry && (repository.retry.Delay != time.Second || repository.retry.Failure.Code != "handler_failed")) {
				t.Fatalf("repository = %+v", repository)
			}
		})
	}
}

func TestNewExecutorRejectsLeaseLongerThanContractMaximum(t *testing.T) {
	_, err := NewExecutor(&repositoryFake{}, []Handler{handlerFake{kind: "sync"}}, RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second}, MaxLeaseDuration+time.Nanosecond)
	if !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("NewExecutor() error = %v", err)
	}
}

func TestNewExecutorRequiresHandlersAndFinalizationReserve(t *testing.T) {
	retry := RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second}
	if _, err := NewExecutor(&repositoryFake{}, nil, retry, time.Minute); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("empty handlers error = %v", err)
	}
	if _, err := NewExecutor(&repositoryFake{}, []Handler{handlerFake{kind: "sync"}}, retry, 2*finalizationReserve, 2*finalizationReserve-finalizationReserve/2); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("deadline without reserve error = %v", err)
	}
}

func TestExecutorDeadlineOnlyBoundsHandler(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	repository := &repositoryFake{claimed: &LeasedJob{Job: DurableJob{ID: jobID, Kind: "sync", Attempt: 1, AvailableAt: now, CreatedAt: now}, Lease: Lease{Token: leaseID, ExpiresAt: now.Add(time.Minute)}}}
	executor, err := NewExecutor(repository, []Handler{deadlineHandler{deadline: make(chan struct{}, 1)}}, RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second}, time.Minute, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := executor.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("outer context was canceled: %v", ctx.Err())
	}
	if repository.retry.Failure.Code != FailureCodeHandlerFailed {
		t.Fatalf("retry failure = %q", repository.retry.Failure.Code)
	}
}

func TestExecutorFinalizesAfterHandlerContextIsCanceled(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	repository := &repositoryFake{claimed: &LeasedJob{Job: DurableJob{ID: jobID, Kind: "sync", Attempt: 1, AvailableAt: now, CreatedAt: now}, Lease: Lease{Token: leaseID, ExpiresAt: now.Add(time.Minute)}}}
	executor, err := NewExecutor(repository, []Handler{handlerFake{kind: "sync"}}, RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Cancel the worker context from the handler. Finalization must still use its
	// own bounded context to persist the completed outcome.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executor.handlers["sync"] = cancelingHandler{cancel: cancel}
	if _, err := executor.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !repository.complete {
		t.Fatal("completion was canceled with the handler context")
	}
	if repository.completeContextErr != nil {
		t.Fatalf("completion context was canceled: %v", repository.completeContextErr)
	}
}

type cancelingHandler struct{ cancel context.CancelFunc }

func (cancelingHandler) Kind() string { return "sync" }
func (h cancelingHandler) Handle(context.Context, DurableJob) error {
	h.cancel()
	return nil
}
