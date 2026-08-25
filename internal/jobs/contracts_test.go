package jobs

import (
	"errors"
	"testing"
	"time"
)

const (
	jobID   = "05829685-9808-4a01-a0dc-744fa83a253f"
	leaseID = "e6e79917-23c7-4f90-98c0-bc0a70cf2d6f"
)

func TestValidateDurableJobUsesUUIDAndOneBasedAttempt(t *testing.T) {
	now := time.Now()
	job := DurableJob{ID: jobID, Kind: "sync", Attempt: 1, AvailableAt: now, CreatedAt: now}
	if err := ValidateDurableJob(job); err != nil {
		t.Fatalf("ValidateDurableJob() error = %v", err)
	}
	job.Attempt = 0
	if err := ValidateDurableJob(job); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("ValidateDurableJob() zero attempt error = %v", err)
	}
}

func TestValidateLeaseDoesNotUseWorkerClock(t *testing.T) {
	lease := Lease{Token: leaseID, ExpiresAt: time.Now().Add(-time.Hour)}
	if err := ValidateLease(lease); err != nil {
		t.Fatalf("ValidateLease() error = %v", err)
	}
}

func TestValidateEnqueueAndOutcomeContracts(t *testing.T) {
	if err := ValidateEnqueueRequest(EnqueueRequest{Kind: "sync", IdempotencyKey: jobID}); err != nil {
		t.Fatalf("ValidateEnqueueRequest() error = %v", err)
	}
	outcome := RetryOutcome{JobID: jobID, LeaseToken: leaseID, Delay: time.Second, Failure: FailureData{Code: FailureCodeHandlerFailed}}
	if err := ValidateRetryOutcome(outcome); err != nil {
		t.Fatalf("ValidateRetryOutcome() error = %v", err)
	}
	outcome.Failure.Code = FailureCode("provider returned https://api.example.test/?token=secret")
	if err := ValidateRetryOutcome(outcome); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("ValidateRetryOutcome() error = %v", err)
	}
}

func TestValidateLeaseDurationUsesSharedMaximum(t *testing.T) {
	for _, duration := range []time.Duration{0, -time.Second, MaxLeaseDuration + time.Nanosecond} {
		if err := ValidateLeaseDuration(duration); !errors.Is(err, ErrInvalidJob) {
			t.Fatalf("ValidateLeaseDuration(%s) error = %v", duration, err)
		}
	}
	if err := ValidateLeaseDuration(MaxLeaseDuration); err != nil {
		t.Fatalf("ValidateLeaseDuration(MaxLeaseDuration) error = %v", err)
	}
}
