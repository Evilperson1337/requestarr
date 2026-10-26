package setup

import (
	"context"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/secrets"
)

func TestLibraryCheckMessageNamesTheSecretSourceWithoutTheValue(t *testing.T) {
	for _, target := range []Target{TargetJellyfin, TargetPlex, TargetRomm} {
		repo := &memoryDrafts{}
		svc := NewService(repo, codeChecker{CheckOK}, Runtime{PublicURL: "https://requestarr.example"})
		svc.UseSecrets(stubSecrets{secrets.Resolved{Value: "probe-value", Source: secrets.SourceStored}})
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
		if !strings.Contains(msg, "stored") || strings.Contains(msg, "probe-value") {
			t.Errorf("%s: message %q", target, msg)
		}
	}
}

func TestNavidromeCheckMessageIsUnchangedUntilItsTicket(t *testing.T) {
	repo := &memoryDrafts{}
	svc := NewService(repo, codeChecker{CheckOK}, Runtime{})
	svc.UseSecrets(stubSecrets{secrets.Resolved{Value: "v", Source: secrets.SourceStored}})
	actor := Actor{ID: "admin-1", Role: "admin"}
	draft, _ := svc.Load(context.Background(), actor)
	draft, err := svc.Validate(context.Background(), actor, draft.Revision, TargetNavidrome)
	if err != nil {
		t.Fatal(err)
	}
	if got := draft.Validations[0].Message; strings.Contains(got, "stored") {
		t.Fatalf("message %q", got)
	}
}
