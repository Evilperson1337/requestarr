//go:build integration

package postgres

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/secrets"
)

func TestStoredLibraryCredentialsSetReplaceClearIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	actor := integrationUser(t, ctx, s)
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM secret_audit_events WHERE actor_user_id=$1`, actor)
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM stored_secrets`)
	})
	_, _ = s.pool.Exec(ctx, `DELETE FROM stored_secrets`)
	sealer, err := secrets.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc := secrets.NewService(s, sealer, nil)
	for _, name := range []secrets.Name{secrets.JellyfinAPIKey, secrets.PlexToken, secrets.RommToken} {
		if err := svc.Set(ctx, actor, name, "first-"+string(name)); err != nil {
			t.Fatalf("%s set: %v", name, err)
		}
		if err := svc.Set(ctx, actor, name, "second-"+string(name)); err != nil {
			t.Fatalf("%s replace: %v", name, err)
		}
		if got, _ := svc.Resolve(ctx, name); got.Value != "second-"+string(name) || got.Source != secrets.SourceStored {
			t.Fatalf("%s resolved %q from %q", name, got.Value, got.Source)
		}
		if err := svc.Clear(ctx, actor, name); err != nil {
			t.Fatalf("%s clear: %v", name, err)
		}
		if got, _ := svc.Resolve(ctx, name); got.Value != "" || got.Source != secrets.SourceNone {
			t.Fatalf("%s after clear: %q from %q", name, got.Value, got.Source)
		}
	}
}
