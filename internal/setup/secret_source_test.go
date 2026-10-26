package setup

import (
	"context"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/secrets"
)

type stubSecrets struct{ resolved secrets.Resolved }

func (s stubSecrets) Resolve(context.Context, secrets.Name) (secrets.Resolved, error) {
	return s.resolved, nil
}

type codeChecker struct{ code CheckCode }

func (c codeChecker) Check(context.Context, Target, Settings) (CheckCode, error) { return c.code, nil }

func TestTMDBCheckMessageNamesTheSecretSource(t *testing.T) {
	for _, tc := range []struct {
		source secrets.Source
		code   CheckCode
		want   string
	}{
		{secrets.SourceEnvironment, CheckOK, "environment"},
		{secrets.SourceStored, CheckOK, "stored"},
		{secrets.SourceShadowed, CheckOK, "shadowed by environment"},
		{secrets.SourceStored, CheckUnavailable, "stored"},
		{secrets.SourceUnreadable, CheckEnvironment, "cannot be read"},
	} {
		repo := &memoryDrafts{}
		svc := NewService(repo, codeChecker{tc.code}, Runtime{PublicURL: "https://requestarr.example"})
		svc.UseSecrets(stubSecrets{secrets.Resolved{Value: "v", Source: tc.source}})
		actor := Actor{ID: "admin-1", Role: "admin"}
		draft, err := svc.Load(context.Background(), actor)
		if err != nil {
			t.Fatal(err)
		}
		draft, err = svc.Validate(context.Background(), actor, draft.Revision, TargetTMDB)
		if err != nil {
			t.Fatal(err)
		}
		msg := draft.Validations[len(draft.Validations)-1].Message
		if !strings.Contains(msg, tc.want) {
			t.Errorf("%s/%s: message %q lacks %q", tc.source, tc.code, msg, tc.want)
		}
		if strings.Contains(msg, "v\"") {
			t.Errorf("message leaks value")
		}
	}
}

func TestOtherTargetsMessagesAreUnchangedBySecrets(t *testing.T) {
	repo := &memoryDrafts{}
	svc := NewService(repo, codeChecker{CheckOK}, Runtime{})
	svc.UseSecrets(stubSecrets{secrets.Resolved{Value: "v", Source: secrets.SourceStored}})
	actor := Actor{ID: "admin-1", Role: "admin"}
	draft, _ := svc.Load(context.Background(), actor)
	draft, err := svc.Validate(context.Background(), actor, draft.Revision, TargetGeneral)
	if err != nil {
		t.Fatal(err)
	}
	if got := draft.Validations[0].Message; got != "Connection succeeded. Draft settings remain inactive." {
		t.Fatalf("message %q", got)
	}
}
