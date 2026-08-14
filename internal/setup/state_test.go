package setup

import (
	"context"
	"errors"
	"testing"
)

type stateReaderFunc func(context.Context) (State, error)

func (f stateReaderFunc) ReadState(ctx context.Context) (State, error) { return f(ctx) }

func TestStepValidation(t *testing.T) {
	tests := []struct {
		step Step
		want bool
	}{
		{step: StepWelcome, want: true},
		{step: StepGeneral, want: true},
		{step: StepAuthentication, want: true},
		{step: StepProviders, want: true},
		{step: StepLibraries, want: true},
		{step: StepNotifications, want: true},
		{step: StepPolicy, want: true},
		{step: StepReview, want: true},
		{step: StepFinish, want: true},
		{step: ""},
		{step: "unknown"},
		{step: "FINISH"},
		{step: " finish"},
	}
	for _, tt := range tests {
		if got := tt.step.Valid(); got != tt.want {
			t.Errorf("Step(%q).Valid() = %t, want %t", tt.step, got, tt.want)
		}
	}
}

func TestServiceComplete(t *testing.T) {
	tests := []struct {
		name         string
		state        State
		readErr      error
		wantComplete bool
		wantErr      error
	}{
		{name: "incomplete", state: State{Current: StepWelcome}},
		{name: "complete true is independent from current step", state: State{Complete: true, Current: StepWelcome}, wantComplete: true},
		{name: "complete false is independent from current step", state: State{Complete: false, Current: StepFinish}},
		{name: "malformed current step", state: State{Complete: true, Current: "malformed"}, wantErr: ErrStateInvalid},
		{name: "missing singleton", readErr: ErrStateMissing, wantErr: ErrStateMissing},
		{name: "duplicate singleton", readErr: ErrStateDuplicate, wantErr: ErrStateDuplicate},
		{name: "database unavailable", readErr: ErrStatusUnavailable, wantErr: ErrStatusUnavailable},
		{name: "database canceled", readErr: ErrStatusCanceled, wantErr: ErrStatusCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(stateReaderFunc(func(context.Context) (State, error) {
				return tt.state, tt.readErr
			}))
			complete, err := service.Complete(context.Background())
			if complete != tt.wantComplete || !errors.Is(err, tt.wantErr) {
				t.Fatalf("Complete() = %t, %v; want %t, %v", complete, err, tt.wantComplete, tt.wantErr)
			}
		})
	}
}

func TestServiceRejectsMissingReaderAndCanceledContext(t *testing.T) {
	if _, err := NewService(nil).Complete(context.Background()); !errors.Is(err, ErrStatusUnavailable) {
		t.Fatalf("Complete() missing reader error = %v", err)
	}

	called := false
	service := NewService(stateReaderFunc(func(context.Context) (State, error) {
		called = true
		return State{}, nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Complete(ctx); !errors.Is(err, ErrStatusCanceled) {
		t.Fatalf("Complete() canceled error = %v", err)
	}
	if called {
		t.Fatal("canceled service call reached persistence")
	}
}
