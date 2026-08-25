package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidJob         = errors.New("invalid durable job")
	ErrInvalidRetryPolicy = errors.New("invalid job retry policy")
)

const (
	maxJobKindLength = 128
	maxPayloadBytes  = 64 << 10
	maxAttempts      = 100
	// MaxLeaseDuration bounds how long a worker may exclusively own a job.
	// Repository implementations must enforce this same contract limit.
	MaxLeaseDuration = time.Hour
)

// DurableJob is the storage-neutral representation of a queued operation.
// Payload is opaque to the queue and must contain no credentials.
type DurableJob struct {
	ID          string
	Kind        string
	Payload     []byte
	Attempt     int
	AvailableAt time.Time
	CreatedAt   time.Time
}

func ValidateDurableJob(job DurableJob) error {
	if !validUUID(job.ID) || !validText(job.Kind, maxJobKindLength) || len(job.Payload) > maxPayloadBytes || job.Attempt < 1 || job.Attempt > maxAttempts || job.AvailableAt.IsZero() || job.CreatedAt.IsZero() {
		return ErrInvalidJob
	}
	return nil
}

// EnqueueRequest describes a new durable job. The repository assigns its UUID,
// timestamps, and initial one-based attempt number. IdempotencyKey identifies
// the logical enqueue operation, rather than a process-local scheduling call.
type EnqueueRequest struct {
	Kind           string
	Payload        []byte
	IdempotencyKey string
}

func ValidateEnqueueRequest(request EnqueueRequest) error {
	if !validText(request.Kind, maxJobKindLength) || len(request.Payload) > maxPayloadBytes || !validUUID(request.IdempotencyKey) {
		return ErrInvalidJob
	}
	return nil
}

// EnqueueResult returns the durable job for both a new enqueue and an
// idempotent replay. Created is true only if this call inserted the job.
type EnqueueResult struct {
	Job     DurableJob
	Created bool
}

// Lease is the exclusive, time-bounded right to execute a job. Its opaque
// token is supplied by the repository and must be presented on every outcome.
type Lease struct {
	Token     string
	ExpiresAt time.Time
}

// ValidateLeaseDuration validates a requested lease duration independently of
// any worker clock. Lease expiry remains the repository's time authority.
func ValidateLeaseDuration(duration time.Duration) error {
	if duration <= 0 || duration > MaxLeaseDuration {
		return fmt.Errorf("%w: lease duration must be between zero and %s", ErrInvalidJob, MaxLeaseDuration)
	}
	return nil
}

func ValidateLease(lease Lease) error {
	// Expiry is evaluated atomically by the repository against its durable time
	// authority. A worker's clock must never reject an otherwise valid claim.
	if !validUUID(lease.Token) || lease.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: invalid lease", ErrInvalidJob)
	}
	return nil
}

type LeasedJob struct {
	Job   DurableJob
	Lease Lease
}

func ValidateLeasedJob(job LeasedJob) error {
	if err := ValidateDurableJob(job.Job); err != nil {
		return err
	}
	return ValidateLease(job.Lease)
}

// RetryPolicy uses deterministic exponential backoff without jitter. A
// repository records the resulting retry time durably; callers may add jitter
// outside this contract if product policy later requires it.
type RetryPolicy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

func (p RetryPolicy) Validate() error {
	if p.MaxAttempts < 1 || p.MaxAttempts > maxAttempts || p.InitialDelay <= 0 || p.MaxDelay < p.InitialDelay {
		return ErrInvalidRetryPolicy
	}
	return nil
}

// Delay returns the wait before retrying the supplied one-based failed attempt.
// The value is capped and does not overflow.
func (p RetryPolicy) Delay(failedAttempt int) (time.Duration, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	if failedAttempt < 1 || failedAttempt >= p.MaxAttempts {
		return 0, fmt.Errorf("%w: attempt is not retryable", ErrInvalidRetryPolicy)
	}
	delay := p.InitialDelay
	for attempt := 1; attempt < failedAttempt && delay < p.MaxDelay; attempt++ {
		if delay > p.MaxDelay/2 {
			delay = p.MaxDelay
			break
		}
		delay *= 2
	}
	return delay, nil
}

// FailureCode is the complete vocabulary of safe durable failure diagnostics.
// It deliberately cannot carry raw errors, URLs, tokens, or provider payloads.
type FailureCode string

const (
	FailureCodeHandlerFailed      FailureCode = "handler_failed"
	FailureCodeHandlerUnavailable FailureCode = "handler_unavailable"
	FailureCodeLeaseExpired       FailureCode = "lease_expired"
)

// FailureData is safe durable diagnostic data. It contains only a known
// category; raw adapter, database, and handler errors are never persisted.
type FailureData struct {
	Code FailureCode
}

func ValidateFailureData(failure FailureData) error {
	switch failure.Code {
	case FailureCodeHandlerFailed, FailureCodeHandlerUnavailable, FailureCodeLeaseExpired:
		return nil
	default:
		return ErrInvalidJob
	}
}

// RetryOutcome and FailureOutcome identify an outcome by durable job and lease
// IDs. Delay is duration policy input: the repository derives durable schedule
// time using its own clock authority.
type RetryOutcome struct {
	JobID      string
	LeaseToken string
	Delay      time.Duration
	Failure    FailureData
}

func ValidateRetryOutcome(outcome RetryOutcome) error {
	if !validUUID(outcome.JobID) || !validUUID(outcome.LeaseToken) || outcome.Delay <= 0 {
		return ErrInvalidJob
	}
	return ValidateFailureData(outcome.Failure)
}

type FailureOutcome struct {
	JobID      string
	LeaseToken string
	Failure    FailureData
}

func ValidateFailureOutcome(outcome FailureOutcome) error {
	if !validUUID(outcome.JobID) || !validUUID(outcome.LeaseToken) {
		return ErrInvalidJob
	}
	return ValidateFailureData(outcome.Failure)
}

// Repository owns durable enqueue, claiming, and outcome transitions.
// Implementations must enforce leases and evaluate due/expiry conditions
// atomically using database time; callers supply neither process timestamps nor
// raw execution errors. ClaimDue may return (nil, nil) when no job is due.
type Repository interface {
	Enqueue(context.Context, EnqueueRequest) (EnqueueResult, error)
	Complete(context.Context, string, string) error
	Retry(context.Context, RetryOutcome) error
	Fail(context.Context, FailureOutcome) error
}

// KnownKindRepository claims only jobs for registered handler kinds. Limiting
// claims at the persistence boundary leaves work for disabled or not-yet-known
// handlers queued rather than terminally consuming it.
type KnownKindRepository interface {
	Repository
	ClaimDue(context.Context, time.Duration, []string) (*LeasedJob, error)
}

// Handler contains job business logic and is intentionally independent of
// scheduling or persistence.
type Handler interface {
	Kind() string
	Handle(context.Context, DurableJob) error
}

func validText(value string, maximum int) bool {
	if !utf8.ValidString(value) || len(value) > maximum || strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
