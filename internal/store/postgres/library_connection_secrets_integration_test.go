//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

func TestLibraryConnectionAndCredentialAreSavedTogetherIntegration(t *testing.T) {
	ctx, s, actor := setupDraftStore(t)
	t.Cleanup(func() {
		_ = s.DeleteLibraryConnection(context.Background(), "plex")
		_ = s.DeleteStoredSecret(context.Background(), actor, secrets.PlexToken)
	})
	sealed := map[secrets.Name]secrets.Sealed{secrets.PlexToken: {Ciphertext: []byte("sealed-one"), KeyID: "k1"}}
	if err := s.SaveLibraryConnection(ctx, actor, &sources.Connection{Slug: "plex", URL: "https://plex.example.test"}, sealed); err != nil {
		t.Fatal(err)
	}
	conns, _ := s.ListLibraryConnections(ctx)
	rows, _ := s.ListStoredSecrets(ctx)
	var stored bool
	for _, r := range rows {
		stored = stored || r.Name == secrets.PlexToken && string(r.Ciphertext) == "sealed-one"
	}
	if conns["plex"].URL != "https://plex.example.test" || !stored {
		t.Fatalf("conns=%+v stored=%v", conns["plex"], stored)
	}
	var audits int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM secret_audit_events WHERE actor_user_id=$1 AND secret_name='plex_token' AND action='set'`, actor).Scan(&audits); err != nil || audits < 1 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}
	// Credential only (an environment-locked address): the saved address is untouched.
	sealed[secrets.PlexToken] = secrets.Sealed{Ciphertext: []byte("sealed-two"), KeyID: "k1"}
	if err := s.SaveLibraryConnection(ctx, actor, nil, sealed); err != nil {
		t.Fatal(err)
	}
	conns, _ = s.ListLibraryConnections(ctx)
	if conns["plex"].URL != "https://plex.example.test" {
		t.Fatalf("address changed: %+v", conns["plex"])
	}
}

func TestLibraryConnectionIsNotSavedWhenItsCredentialFailsIntegration(t *testing.T) {
	ctx, s, _ := setupDraftStore(t)
	t.Cleanup(func() { _ = s.DeleteLibraryConnection(context.Background(), "romm") })
	// An actor that is not a user makes the credential write fail on its foreign key.
	sealed := map[secrets.Name]secrets.Sealed{secrets.RommToken: {Ciphertext: []byte("x"), KeyID: "k1"}}
	err := s.SaveLibraryConnection(ctx, "00000000-0000-4000-8000-000000000000", &sources.Connection{Slug: "romm", URL: "https://romm.example.test"}, sealed)
	if err == nil {
		t.Fatal("the credential write should have failed")
	}
	if conns, _ := s.ListLibraryConnections(ctx); conns["romm"].Slug != "" {
		t.Fatalf("the address was applied without its credential: %+v", conns["romm"])
	}
}
