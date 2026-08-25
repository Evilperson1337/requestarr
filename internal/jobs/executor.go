package jobs

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Executor claims at most one due job per call. It intentionally does not
// start goroutines or poll, leaving process lifecycle and worker count to the
// application layer.
type Executor struct {
	repository   KnownKindRepository
	handlers     map[string]Handler
	retry        RetryPolicy
	leaseFor     time.Duration
	executionFor time.Duration
}

const finalizationReserve = 5 * time.Second

func NewExecutor(repository KnownKindRepository, handlers []Handler, retry RetryPolicy, leaseFor time.Duration, executionFor ...time.Duration) (*Executor, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: a known-kind repository and lease duration are required", ErrInvalidJob)
	}
	if err := ValidateLeaseDuration(leaseFor); err != nil {
		return nil, err
	}
	if err := retry.Validate(); err != nil {
		return nil, err
	}
	if leaseFor <= finalizationReserve {
		return nil, fmt.Errorf("%w: lease duration must exceed the %s finalization reserve", ErrInvalidJob, finalizationReserve)
	}
	deadline := leaseFor - finalizationReserve
	if len(executionFor) > 1 || (len(executionFor) == 1 && (executionFor[0] <= 0 || executionFor[0] > deadline)) {
		return nil, fmt.Errorf("%w: execution deadline must be positive and leave a %s finalization reserve", ErrInvalidJob, finalizationReserve)
	}
	if len(executionFor) == 1 {
		deadline = executionFor[0]
	}
	registered := make(map[string]Handler, len(handlers))
	for _, handler := range handlers {
		if handler == nil || !validText(handler.Kind(), maxJobKindLength) {
			return nil, fmt.Errorf("%w: handler kind is required", ErrInvalidJob)
		}
		if _, exists := registered[handler.Kind()]; exists {
			return nil, fmt.Errorf("%w: duplicate handler kind", ErrInvalidJob)
		}
		registered[handler.Kind()] = handler
	}
	if len(registered) == 0 {
		return nil, fmt.Errorf("%w: at least one handler is required", ErrInvalidJob)
	}
	return &Executor{repository: repository, handlers: registered, retry: retry, leaseFor: leaseFor, executionFor: deadline}, nil
}

// RunOnce returns ran=false when no job is due. Handler errors are persisted as
// retry/final outcomes and therefore are not returned; repository failures are.
func (e *Executor) RunOnce(ctx context.Context) (ran bool, err error) {
	if e == nil {
		return false, fmt.Errorf("%w: executor is nil", ErrInvalidJob)
	}
	leased, err := e.repository.ClaimDue(ctx, e.leaseFor, e.handlerKinds())
	if err != nil || leased == nil {
		return false, err
	}
	if err := ValidateLeasedJob(*leased); err != nil {
		return true, fmt.Errorf("claimed job: %w", err)
	}
	handler, exists := e.handlers[leased.Job.Kind]
	if !exists {
		return true, fmt.Errorf("claimed unregistered handler kind %q", leased.Job.Kind)
	}
	handlerCtx, cancel := context.WithTimeout(ctx, e.executionFor)
	handlerErr := handler.Handle(handlerCtx, leased.Job)
	cancel()
	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), finalizationReserve)
	defer finalizeCancel()
	if handlerErr == nil {
		return true, e.repository.Complete(finalizeCtx, leased.Job.ID, leased.Lease.Token)
	} else if leased.Job.Attempt < e.retry.MaxAttempts {
		delay, delayErr := e.retry.Delay(leased.Job.Attempt)
		if delayErr != nil {
			return true, delayErr
		}
		return true, e.repository.Retry(finalizeCtx, RetryOutcome{JobID: leased.Job.ID, LeaseToken: leased.Lease.Token, Delay: delay, Failure: executionFailure(handlerErr)})
	} else {
		return true, e.repository.Fail(finalizeCtx, FailureOutcome{JobID: leased.Job.ID, LeaseToken: leased.Lease.Token, Failure: executionFailure(handlerErr)})
	}
}

func (e *Executor) handlerKinds() []string {
	kinds := make([]string, 0, len(e.handlers))
	for kind := range e.handlers {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

func executionFailure(error) FailureData {
	// Handler errors can contain provider responses or credentials. Preserve only
	// a stable category until handlers expose explicitly redacted error types.
	return FailureData{Code: FailureCodeHandlerFailed}
}
