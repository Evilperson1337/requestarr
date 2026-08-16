//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/secrets"
	"git.example.com/evilperson1337/Requestarr/internal/setup"
)

func TestSetupAuthenticationEncryptedWorkflowAndGeneralCrossPreservation(t *testing.T) {
	databaseURL, cleanup := encryptedTestSchema(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := migratedEncryptedPool(t, ctx, databaseURL)
	defer pool.Close()
	cipher := secrets.New([32]byte{19})
	keyID := cipher.KeyID()
	secretRepository := NewEncryptedSecretRepository(pool, &keyID)
	if err := secretRepository.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repository := NewSetupWorkflowRepository(pool, secretRepository, cipher)
	digest := setup.Digest{31: 7}
	if _, err := pool.pool.Exec(ctx, `INSERT INTO setup_sessions(id,token_digest,created_at,expires_at) VALUES($1,$2,clock_timestamp(),clock_timestamp()+interval '15 minutes')`, persistenceID(230), digest[:]); err != nil {
		t.Fatal(err)
	}
	general := setup.General{InstanceName: "Requestarr", PublicURL: "https://example.com", Timezone: "UTC"}
	if _, err := repository.SaveGeneral(ctx, digest, general); err != nil {
		t.Fatal(err)
	}
	authentication := setup.Authentication{Issuer: "https://id.example.com/realms/main", ClientID: "requestarr", ClientType: setup.ClientTypeConfidential, Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: "viewer"}
	replace, err := setup.ReplaceSecret("integration-secret-one")
	if err != nil {
		t.Fatal(err)
	}
	first, err := repository.SaveAuthentication(ctx, digest, authentication, replace)
	if err != nil || !first.State.ClientSecretConfigured || first.State.General == nil || *first.State.General != general {
		t.Fatalf("first save=%#v err=%v", first, err)
	}
	var nonce, ciphertext []byte
	var updated time.Time
	if err := pool.pool.QueryRow(ctx, `SELECT nonce,ciphertext,updated_at FROM encrypted_secrets WHERE namespace='setup.authentication' AND secret_name='client_secret'`).Scan(&nonce, &ciphertext, &updated); err != nil {
		t.Fatal(err)
	}
	var events int
	if err := pool.pool.QueryRow(ctx, `SELECT count(*) FROM setup_workflow_events WHERE event_type='authentication_saved'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	same, _ := setup.ReplaceSecret("integration-secret-one")
	if _, err := repository.SaveAuthentication(ctx, digest, authentication, same); err != nil {
		t.Fatal(err)
	}
	var nonce2, ciphertext2 []byte
	var updated2 time.Time
	var events2 int
	if err := pool.pool.QueryRow(ctx, `SELECT nonce,ciphertext,updated_at,(SELECT count(*) FROM setup_workflow_events WHERE event_type='authentication_saved') FROM encrypted_secrets WHERE namespace='setup.authentication' AND secret_name='client_secret'`).Scan(&nonce2, &ciphertext2, &updated2, &events2); err != nil {
		t.Fatal(err)
	}
	if string(nonce) != string(nonce2) || string(ciphertext) != string(ciphertext2) || !updated.Equal(updated2) || events != events2 {
		t.Fatal("same-secret save changed envelope, timestamp, or audit")
	}
	different, _ := setup.ReplaceSecret("integration-secret-two")
	if _, err := repository.SaveAuthentication(ctx, digest, authentication, different); err != nil {
		t.Fatal(err)
	}
	changedGeneral := setup.General{InstanceName: "Changed", PublicURL: "https://example.com", Timezone: "UTC"}
	result, err := repository.SaveGeneral(ctx, digest, changedGeneral)
	if err != nil || !result.State.ClientSecretConfigured || result.State.Authentication == nil || !setup.AuthenticationEqual(*result.State.Authentication, authentication) {
		t.Fatalf("changed General authoritative response=%#v err=%v", result, err)
	}
	result, err = repository.SaveGeneral(ctx, digest, changedGeneral)
	if err != nil || !result.State.ClientSecretConfigured {
		t.Fatalf("no-op General authoritative response=%#v err=%v", result, err)
	}
	public := authentication
	public.ClientType = setup.ClientTypePublic
	cleared, err := repository.SaveAuthentication(ctx, digest, public, setup.ClearSecret())
	if err != nil || cleared.State.ClientSecretConfigured {
		t.Fatalf("public transition=%#v err=%v", cleared, err)
	}
	if _, err := secretRepository.Read(ctx, setupAuthenticationSecretContext); !errors.Is(err, ErrEncryptedSecretNotFound) {
		t.Fatalf("public transition retained secret: %v", err)
	}
	if _, err := repository.SaveAuthentication(ctx, digest, authentication, setup.PreserveSecret()); !errors.Is(err, setup.ErrInvalidPersistenceInput) {
		t.Fatalf("confidential preserve without envelope error=%v", err)
	}
}
