package setup

import (
	"context"
	"errors"
)

type Step string

const (
	StepWelcome        Step = "welcome"
	StepGeneral        Step = "general"
	StepAuthentication Step = "authentication"
	StepProviders      Step = "providers"
	StepLibraries      Step = "libraries"
	StepNotifications  Step = "notifications"
	StepPolicy         Step = "policy"
	StepReview         Step = "review"
	StepFinish         Step = "finish"
)

type State struct {
	Complete bool
	Current  Step
}

var (
	// ErrStatusUnavailable identifies a setup-state read that could not be
	// completed because its backing store was unavailable.
	ErrStatusUnavailable = errors.New("setup status unavailable")
	// ErrStatusCanceled identifies a setup-state read stopped by cancellation.
	ErrStatusCanceled = errors.New("setup status read canceled")
	// ErrStateMissing identifies an absent required singleton row.
	ErrStateMissing = errors.New("setup state missing")
	// ErrStateDuplicate identifies an impossible duplicate singleton result.
	ErrStateDuplicate = errors.New("setup state duplicated")
	// ErrStateInvalid identifies malformed persisted setup state.
	ErrStateInvalid = errors.New("setup state invalid")
)

// Valid reports whether a setup step is part of the persisted state contract.
func (s Step) Valid() bool {
	switch s {
	case StepWelcome, StepGeneral, StepAuthentication, StepProviders, StepLibraries,
		StepNotifications, StepPolicy, StepReview, StepFinish:
		return true
	default:
		return false
	}
}

// Validate rejects malformed persisted setup state without inferring completion
// from the current step.
func (s State) Validate() error {
	if !s.Current.Valid() {
		return ErrStateInvalid
	}
	return nil
}

// StateReader is the narrow persistence boundary consumed by setup semantics.
type StateReader interface {
	ReadState(context.Context) (State, error)
}

// Service validates authoritative persisted state and exposes only completion.
type Service struct {
	states StateReader
}

func NewService(states StateReader) *Service {
	return &Service{states: states}
}

// Complete returns the persisted completion flag only after the entire state
// row satisfies the setup domain contract.
func (s *Service) Complete(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, ErrStatusCanceled
	}
	if s == nil || s.states == nil {
		return false, ErrStatusUnavailable
	}
	state, err := s.states.ReadState(ctx)
	if err != nil {
		return false, err
	}
	if err := state.Validate(); err != nil {
		return false, err
	}
	return state.Complete, nil
}
