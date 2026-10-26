package setup

import (
	"context"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/secrets"
)

// An unreadable stored secret must be reported whatever the check code, so a
// lost master key is visible even when the step is Incomplete or Disabled.
func TestUnreadableStoredSecretIsReportedForEveryCheckCode(t *testing.T) {
	for _, code := range []CheckCode{CheckOK, CheckUnavailable, CheckEnvironment, CheckIncomplete, CheckDisabled} {
		for _, target := range []Target{TargetTMDB, TargetIGDB, TargetNotifications} {
			svc := NewService(&memoryDrafts{}, codeChecker{code}, Runtime{})
			svc.UseSecrets(stubSecrets{secrets.Resolved{Source: secrets.SourceUnreadable}})
			actor := Actor{ID: "admin-1", Role: "admin"}
			draft, err := svc.Load(context.Background(), actor)
			if err != nil {
				t.Fatal(err)
			}
			draft, err = svc.Validate(context.Background(), actor, draft.Revision, target)
			if err != nil {
				t.Fatal(err)
			}
			msg := draft.Validations[len(draft.Validations)-1].Message
			if !strings.Contains(msg, "cannot be read") {
				t.Errorf("%s/%s: message %q does not report the unreadable secret", target, code, msg)
			}
		}
	}
}

func TestHealthySecretNotesStayOffDisabledAndIncompleteChecks(t *testing.T) {
	for _, code := range []CheckCode{CheckIncomplete, CheckDisabled} {
		svc := NewService(&memoryDrafts{}, codeChecker{code}, Runtime{})
		svc.UseSecrets(stubSecrets{secrets.Resolved{Value: "v", Source: secrets.SourceStored}})
		actor := Actor{ID: "admin-1", Role: "admin"}
		draft, _ := svc.Load(context.Background(), actor)
		draft, err := svc.Validate(context.Background(), actor, draft.Revision, TargetTMDB)
		if err != nil {
			t.Fatal(err)
		}
		if msg := draft.Validations[len(draft.Validations)-1].Message; strings.Contains(msg, "stored") {
			t.Errorf("%s: message %q should not claim a secret was checked", code, msg)
		}
	}
}
