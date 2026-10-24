package setup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
)

func finishFixture(t *testing.T) (*Service, *memoryDrafts, Actor, Draft) {
	t.Helper()
	repo := &memoryDrafts{}
	service := NewService(repo, fakeChecker{}, Runtime{PublicURL: "https://requestarr.example", OIDCIssuer: "https://issuer.example", OIDCClientID: "requestarr"})
	actor := Actor{ID: "admin-1", Role: "admin"}
	draft, err := service.Load(context.Background(), actor)
	if err != nil {
		t.Fatal(err)
	}
	return service, repo, actor, draft
}

func checkRequired(t *testing.T, service *Service, actor Actor, draft Draft) Draft {
	t.Helper()
	for _, target := range draft.Settings.RequiredTargets() {
		var err error
		if draft, err = service.Validate(context.Background(), actor, draft.Revision, target); err != nil {
			t.Fatal(err)
		}
	}
	return draft
}

func TestFinishCompletesFlowAndWizardStopsAcceptingEdits(t *testing.T) {
	ctx := context.Background()
	service, _, actor, draft := finishFixture(t)
	settings := draft.Settings
	settings.Notifications = NotificationSettings{AppriseEnabled: true, AppriseURL: "https://apprise.example", AppriseConfigKeyEnv: AppriseConfigKeyEnv}
	settings.Policy.AutoApproveMediaTypes = []domain.MediaType{domain.MediaMovie}
	saved, err := service.Save(ctx, actor, SaveInput{ExpectedRevision: draft.Revision, CurrentStep: StepReview, Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Blockers) == 0 {
		t.Fatal("unchecked draft should list blockers")
	}
	checked := checkRequired(t, service, actor, saved)
	if len(checked.Blockers) != 0 {
		t.Fatalf("blockers after passing checks: %+v", checked.Blockers)
	}
	done, err := service.Finish(ctx, actor, checked.Revision)
	if err != nil || done.CompletedAt == nil {
		t.Fatalf("finish: %+v %v", done, err)
	}
	if again, err := service.Finish(ctx, actor, checked.Revision); err != nil || again.CompletedAt == nil {
		t.Fatalf("finish should be idempotent: %v", err)
	}
	if _, err := service.Save(ctx, actor, SaveInput{ExpectedRevision: done.Revision, CurrentStep: StepGeneral, Settings: settings}); !errors.Is(err, ErrCompleted) {
		t.Fatalf("save after finish: %v", err)
	}
	if _, err := service.Validate(ctx, actor, done.Revision, TargetGeneral); !errors.Is(err, ErrCompleted) {
		t.Fatalf("validate after finish: %v", err)
	}
}

func TestFinishRefusesFailingMissingAndExpiredChecks(t *testing.T) {
	ctx := context.Background()
	service, repo, actor, draft := finishFixture(t)
	if _, err := service.Finish(ctx, actor, draft.Revision); !errors.Is(err, ErrNotReady) {
		t.Fatalf("unchecked draft finished: %v", err)
	}
	failing := NewService(repo, fakeChecker{err: errors.New("down")}, Runtime{})
	checked := checkRequired(t, failing, actor, draft)
	if _, err := service.Finish(ctx, actor, checked.Revision); !errors.Is(err, ErrNotReady) {
		t.Fatalf("failing checks finished: %v", err)
	}
	if repo.draft.CompletedAt != nil {
		t.Fatal("setup marked complete despite failing checks")
	}
	repo.draft.Validations = nil
	checked = checkRequired(t, service, actor, repo.draft)
	for i := range repo.draft.Validations {
		repo.draft.Validations[i].ExpiresAt = time.Now().Add(-time.Minute)
	}
	if _, err := service.Finish(ctx, actor, checked.Revision); !errors.Is(err, ErrNotReady) {
		t.Fatalf("expired checks finished: %v", err)
	}
}

func TestFinishRejectsStaleRevisionAndNonAdmins(t *testing.T) {
	ctx := context.Background()
	service, repo, actor, draft := finishFixture(t)
	checkRequired(t, service, actor, draft)
	if _, err := service.Finish(ctx, actor, draft.Revision+1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	for _, other := range []Actor{{}, {ID: "u", Role: "viewer"}, {ID: "u", Role: "requester"}} {
		if _, err := service.Finish(ctx, other, draft.Revision); !errors.Is(err, ErrForbidden) {
			t.Fatalf("non-admin finish %+v: %v", other, err)
		}
	}
	if repo.draft.CompletedAt != nil {
		t.Fatal("completed by non-admin or stale call")
	}
}

func TestNotificationAndPolicySettingsAreClosed(t *testing.T) {
	_, _, _, draft := finishFixture(t)
	for name, change := range map[string]func(*Settings){
		"arbitrary config key env": func(s *Settings) { s.Notifications.AppriseConfigKeyEnv = "DATABASE_PASSWORD" },
		"credential Apprise URL":   func(s *Settings) { s.Notifications.AppriseURL = "https://u:p@apprise.example" },
		"unknown auto-approve":     func(s *Settings) { s.Policy.AutoApproveMediaTypes = []domain.MediaType{"podcast"} },
		"duplicate auto-approve": func(s *Settings) {
			s.Policy.AutoApproveMediaTypes = []domain.MediaType{domain.MediaTV, domain.MediaTV}
		},
	} {
		bad := draft.Settings
		change(&bad)
		if err := bad.Validate(false); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	if err := draft.Settings.Validate(false); err != nil {
		t.Fatalf("default settings invalid: %v", err)
	}
}
