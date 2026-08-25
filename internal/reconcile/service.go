// Package reconcile applies targeted, explainable inventory-match policy.
// It intentionally has no inventory adapter, scheduler, HTTP transport, or
// request-workflow mutation. Library sync code supplies one candidate at a time.
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
)

var (
	ErrInvalidInput = errors.New("reconciliation input is invalid")
	ErrCanceled     = errors.New("reconciliation operation canceled")
)

// AvailabilityState is an explicit projection decision. It never changes the
// request workflow status; a future adapter owns durable request availability.
type AvailabilityState string

const (
	AvailabilityUnchanged AvailabilityState = "unchanged"
	AvailabilitySatisfied AvailabilityState = "satisfied"
	AvailabilityReview    AvailabilityState = "review_required"
)

// Target identifies the one request and one authoritative inventory candidate
// involved in a reconciliation decision.
type Target struct {
	RequestID       string
	SourceID        string
	InventoryItemID string
}

// Candidate is normalized matching input. Conflict flags must be asserted by
// the caller's bounded matcher, rather than inferred from opaque source data.
type Candidate struct {
	Evidence         domain.MatchEvidence
	NarratorConflict bool
	EditionConflict  bool
	ObservedAt       time.Time
}

// Input models a single targeted reconciliation attempt. SourceFailed means
// the inventory source did not provide a candidate; it is deliberately a
// non-mutating outcome so temporary outages cannot remove known availability.
type Input struct {
	Target       Target
	Candidate    Candidate
	SourceFailed bool
}

// Outcome is a policy result, separate from the request workflow status.
type Outcome struct {
	Availability AvailabilityState
	AutoFulfill  bool
	Evidence     domain.MatchEvidence
}

// EvidenceRepository is consumer-owned and deliberately narrow. Implementors
// atomically persist an active evidence association and its audit event.
type EvidenceRepository interface {
	Activate(context.Context, Activation) (Change, error)
	Deactivate(context.Context, Target, time.Time) (Change, error)
}

// Activation is the sanitized evidence ledger write contract.
type Activation struct {
	Target     Target
	Evidence   domain.MatchEvidence
	ObservedAt time.Time
}

// Change describes whether a durable mutation occurred. A no-op emits no audit
// event and is safe for retried targeted reconciliation.
type Change struct {
	AssociationID string
	Changed       bool
}

// Service evaluates policy before asking persistence to mutate the evidence
// ledger. It never persists a source-failure attempt and never deactivates it.
type Service struct{ evidence EvidenceRepository }

func NewService(evidence EvidenceRepository) *Service { return &Service{evidence: evidence} }

func (s *Service) Reconcile(ctx context.Context, input Input) (Outcome, Change, error) {
	if err := ctx.Err(); err != nil {
		return Outcome{}, Change{}, fmt.Errorf("%w: %v", ErrCanceled, err)
	}
	if err := validateTarget(input.Target); err != nil {
		return Outcome{}, Change{}, err
	}
	if input.SourceFailed {
		return Outcome{Availability: AvailabilityUnchanged}, Change{}, nil
	}
	if s == nil || s.evidence == nil {
		return Outcome{}, Change{}, fmt.Errorf("%w: evidence repository unavailable", ErrInvalidInput)
	}
	evidence, err := domain.NormalizeMatchEvidence(input.Candidate.Evidence)
	if err != nil {
		return Outcome{}, Change{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if input.Candidate.ObservedAt.IsZero() {
		return Outcome{}, Change{}, fmt.Errorf("%w: observed time is required", ErrInvalidInput)
	}
	outcome := Outcome{Evidence: evidence, Availability: AvailabilityReview}
	if !input.Candidate.NarratorConflict && !input.Candidate.EditionConflict && evidence.CanAutoFulfill() {
		outcome.Availability, outcome.AutoFulfill = AvailabilitySatisfied, true
	}
	change, err := s.evidence.Activate(ctx, Activation{Target: input.Target, Evidence: evidence, ObservedAt: input.Candidate.ObservedAt.UTC()})
	if err != nil {
		return Outcome{}, Change{}, err
	}
	return outcome, change, nil
}

func validateTarget(target Target) error {
	if strings.TrimSpace(target.RequestID) == "" || strings.TrimSpace(target.SourceID) == "" || strings.TrimSpace(target.InventoryItemID) == "" {
		return fmt.Errorf("%w: request, source, and inventory IDs are required", ErrInvalidInput)
	}
	return nil
}
