package setup

import (
	"context"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/secrets"
)

func TestIGDBAndAppriseCheckMessagesNameTheSecretSource(t *testing.T) {
	for _, tc := range []struct {
		target Target
		source secrets.Source
		want   string
	}{
		{TargetIGDB, secrets.SourceStored, "stored client secret"},
		{TargetIGDB, secrets.SourceEnvironment, "client secret from the environment"},
		{TargetIGDB, secrets.SourceShadowed, "shadowed by environment"},
		{TargetIGDB, secrets.SourceUnreadable, "cannot be read"},
		{TargetNotifications, secrets.SourceStored, "stored configuration key"},
		{TargetNotifications, secrets.SourceShadowed, "shadowed by environment"},
		{TargetNotifications, secrets.SourceUnreadable, "cannot be read"},
	} {
		svc := NewService(&memoryDrafts{}, codeChecker{CheckOK}, Runtime{})
		svc.UseSecrets(stubSecrets{secrets.Resolved{Value: "v", Source: tc.source}})
		actor := Actor{ID: "admin-1", Role: "admin"}
		draft, err := svc.Load(context.Background(), actor)
		if err != nil {
			t.Fatal(err)
		}
		draft, err = svc.Validate(context.Background(), actor, draft.Revision, tc.target)
		if err != nil {
			t.Fatal(err)
		}
		msg := draft.Validations[len(draft.Validations)-1].Message
		if !strings.Contains(msg, tc.want) {
			t.Errorf("%s/%s: message %q lacks %q", tc.target, tc.source, msg, tc.want)
		}
	}
}
