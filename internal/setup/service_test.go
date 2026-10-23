package setup

import (
	"context"
	"errors"
	"testing"
)

type memoryDrafts struct {
	draft Draft
	reads int
}

func (r *memoryDrafts) LoadSetupDraft(_ context.Context, initial Draft, _ string) (Draft, error) {
	r.reads++
	if r.draft.Revision == 0 {
		r.draft = initial
	}
	return r.draft, nil
}

func (r *memoryDrafts) SaveSetupDraft(_ context.Context, actor string, expected int64, step Step, settings Settings) (Draft, error) {
	if expected != r.draft.Revision {
		return Draft{}, ErrConflict
	}
	r.draft.Revision++
	r.draft.CurrentStep, r.draft.Settings = step, settings
	r.draft.Validations = nil
	return r.draft, nil
}

func (r *memoryDrafts) RecordSetupValidation(_ context.Context, actor string, expected int64, result Validation) (Draft, error) {
	if expected != r.draft.Revision {
		return Draft{}, ErrConflict
	}
	r.draft.Validations = append(r.draft.Validations, result)
	return r.draft, nil
}

type fakeChecker struct{ err error }

func (c fakeChecker) Check(context.Context, Target, Settings) (CheckCode, error) {
	return CheckOK, c.err
}

func TestDraftResumesAcrossServicesAndConnectionFailureKeepsSettings(t *testing.T) {
	ctx := context.Background()
	repo := &memoryDrafts{}
	runtime := Runtime{PublicURL: "https://requestarr.example", OIDCIssuer: "https://issuer.example", OIDCClientID: "requestarr"}
	actor := Actor{ID: "admin-1", Role: "admin"}
	first := NewService(repo, fakeChecker{}, runtime)
	draft, err := first.Load(ctx, actor)
	if err != nil || draft.Revision != 1 || draft.CurrentStep != StepGeneral {
		t.Fatalf("initial draft=%+v err=%v", draft, err)
	}
	settings := draft.Settings
	settings.General.InstanceName = "Shared library"
	saved, err := first.Save(ctx, actor, SaveInput{ExpectedRevision: draft.Revision, CurrentStep: StepAuthentication, Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	second := NewService(repo, fakeChecker{err: errors.New("upstream error containing a secret")}, runtime)
	resumed, err := second.Load(ctx, actor)
	if err != nil || resumed.CurrentStep != StepAuthentication || resumed.Settings.General.InstanceName != "Shared library" || resumed.Revision != saved.Revision {
		t.Fatalf("resume=%+v err=%v", resumed, err)
	}
	checked, err := second.Validate(ctx, actor, resumed.Revision, TargetAuthentication)
	if err != nil || len(checked.Validations) != 1 || checked.Validations[0].Valid || checked.Validations[0].Code != CheckUnavailable {
		t.Fatalf("failed connection should produce a saved safe result: %+v err=%v", checked, err)
	}
	if checked.Validations[0].Message != "Connection check failed. Verify the service address, network access, and server environment, then retry." {
		t.Fatalf("unsafe or unclear error: %q", checked.Validations[0].Message)
	}
	if checked.Settings != resumed.Settings || checked.CurrentStep != StepAuthentication {
		t.Fatal("failed validation discarded or advanced the draft")
	}
}
