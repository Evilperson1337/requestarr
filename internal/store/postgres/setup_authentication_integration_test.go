//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
	"git.example.com/evilperson1337/Requestarr/internal/auth/oidc"
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
	if err != nil || !first.State.ClientSecretConfigured || first.State.General == nil || *first.State.General != general || first.State.AuthenticationRevision != 1 {
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

func TestSetupAuthenticationValidationRevisionEvidenceRateConcurrencyAndStaleness(t *testing.T) {
	databaseURL, cleanup := encryptedTestSchema(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := migratedEncryptedPool(t, ctx, databaseURL)
	defer pool.Close()
	cipher := secrets.New([32]byte{23})
	keyID := cipher.KeyID()
	secretRepository := NewEncryptedSecretRepository(pool, &keyID)
	if err := secretRepository.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repository := NewSetupWorkflowRepository(pool, secretRepository, cipher)
	digest := setup.Digest{31: 9}
	global := setup.Digest{31: 10}
	if _, err := pool.pool.Exec(ctx, `INSERT INTO setup_sessions(id,token_digest,created_at,expires_at) VALUES($1,$2,clock_timestamp(),clock_timestamp()+interval '15 minutes')`, persistenceID(231), digest[:]); err != nil {
		t.Fatal(err)
	}
	authentication := setup.Authentication{Issuer: "https://issuer.example/realm", ClientID: "requestarr", ClientType: setup.ClientTypePublic, Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: "viewer"}
	saved, err := repository.SaveAuthentication(ctx, digest, authentication, setup.ClearSecret())
	if err != nil || saved.State.AuthenticationRevision != 1 {
		t.Fatalf("save=%+v err=%v", saved, err)
	}

	snapshot, err := repository.ReserveAuthenticationValidation(ctx, digest, global, 1)
	if err != nil || snapshot.Outcome != setup.AuthenticationValidationSucceeded || snapshot.Revision != 1 || !setup.AuthenticationEqual(snapshot.Authentication, authentication) {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	evidence := &oidc.Evidence{PolicyVersion: oidc.PolicyVersion, Issuer: authentication.Issuer, MetadataDigest: strings.Repeat("ab", 32)}
	result, err := repository.FinishAuthenticationValidation(ctx, digest, snapshot, evidence, 0)
	if err != nil || result.Outcome != setup.AuthenticationValidationSucceeded || result.Evidence == nil {
		t.Fatalf("finish=%+v err=%v", result, err)
	}
	read, err := repository.ReadWorkflow(ctx, digest)
	if err != nil || read.State.AuthenticationRevision != 1 || read.State.AuthenticationEvidence == nil || read.State.AuthenticationEvidence.AuthenticationRevision != 1 {
		t.Fatalf("read=%+v err=%v", read, err)
	}
	noOp, err := repository.SaveAuthentication(ctx, digest, authentication, setup.ClearSecret())
	if err != nil || noOp.State.AuthenticationRevision != 1 {
		t.Fatalf("no-op=%+v err=%v", noOp, err)
	}
	var evidenceRows int
	if err := pool.pool.QueryRow(ctx, `SELECT count(*) FROM setup_authentication_validation_evidence`).Scan(&evidenceRows); err != nil || evidenceRows != 1 {
		t.Fatalf("evidence after no-op=%d err=%v", evidenceRows, err)
	}

	changed := authentication
	changed.ClientID = "requestarr-v2"
	changedResult, err := repository.SaveAuthentication(ctx, digest, changed, setup.ClearSecret())
	if err != nil || changedResult.State.AuthenticationRevision != 2 {
		t.Fatalf("changed=%+v err=%v", changedResult, err)
	}
	if err := pool.pool.QueryRow(ctx, `SELECT count(*) FROM setup_authentication_validation_evidence`).Scan(&evidenceRows); err != nil || evidenceRows != 0 {
		t.Fatalf("evidence after effective save=%d err=%v", evidenceRows, err)
	}
	stale, err := repository.FinishAuthenticationValidation(ctx, digest, snapshot, evidence, 0)
	if err != nil || stale.Outcome != setup.AuthenticationValidationStale || stale.Revision != 2 {
		t.Fatalf("late finish=%+v err=%v", stale, err)
	}

	latest, err := repository.ReserveAuthenticationValidation(ctx, digest, global, 2)
	if err != nil || latest.Outcome != setup.AuthenticationValidationSucceeded {
		t.Fatalf("latest snapshot=%+v err=%v", latest, err)
	}
	latestEvidence := &oidc.Evidence{PolicyVersion: oidc.PolicyVersion, Issuer: changed.Issuer, MetadataDigest: strings.Repeat("cd", 32)}
	start := make(chan struct{})
	results := make(chan setup.AuthenticationValidationResult, 2)
	errs := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			got, finishErr := repository.FinishAuthenticationValidation(ctx, digest, latest, latestEvidence, 0)
			results <- got
			errs <- finishErr
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errs)
	for finishErr := range errs {
		if finishErr != nil {
			t.Fatalf("concurrent equivalent finish: %v", finishErr)
		}
	}
	for got := range results {
		if got.Outcome != setup.AuthenticationValidationSucceeded || got.Evidence == nil {
			t.Fatalf("concurrent result=%+v", got)
		}
	}
	var successEvents int
	if err := pool.pool.QueryRow(ctx, `SELECT count(*) FROM setup_workflow_events WHERE event_type='authentication_validation_succeeded'`).Scan(&successEvents); err != nil || successEvents != 2 {
		// One success belongs to revision 1 and exactly one to revision 2.
		t.Fatalf("success events=%d err=%v", successEvents, err)
	}

	otherDigest := setup.Digest{31: 11}
	if _, err := pool.pool.Exec(ctx, `INSERT INTO setup_sessions(id,token_digest,created_at,expires_at) VALUES($1,$2,clock_timestamp(),clock_timestamp()+interval '15 minutes')`, persistenceID(232), otherDigest[:]); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 6; attempt++ {
		got, reserveErr := repository.ReserveAuthenticationValidation(ctx, otherDigest, global, 2)
		if reserveErr != nil {
			t.Fatal(reserveErr)
		}
		if attempt <= 5 && got.Outcome != setup.AuthenticationValidationSucceeded {
			t.Fatalf("attempt %d outcome=%v", attempt, got.Outcome)
		}
		if attempt == 6 && (got.Outcome != setup.AuthenticationValidationRateLimited || got.RetryAfter <= 0 || got.RetryAfter > 5*time.Minute) {
			t.Fatalf("limited attempt=%+v", got)
		}
	}
	var globalBefore int
	if err := pool.pool.QueryRow(ctx, `SELECT attempt_count FROM setup_authentication_validation_rate_limits WHERE scope='global' AND key_digest=$1`, global[:]).Scan(&globalBefore); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 10; attempt++ {
		got, reserveErr := repository.ReserveAuthenticationValidation(ctx, otherDigest, global, 2)
		if reserveErr != nil || got.Outcome != setup.AuthenticationValidationRateLimited {
			t.Fatalf("repeated session-overlimit attempt=%d result=%+v err=%v", attempt, got, reserveErr)
		}
	}
	var globalAfter int
	if err := pool.pool.QueryRow(ctx, `SELECT attempt_count FROM setup_authentication_validation_rate_limits WHERE scope='global' AND key_digest=$1`, global[:]).Scan(&globalAfter); err != nil {
		t.Fatal(err)
	}
	if globalAfter != globalBefore {
		t.Fatalf("session-overlimit attempts consumed global capacity: before=%d after=%d", globalBefore, globalAfter)
	}
	thirdDigest := setup.Digest{31: 12}
	if _, err := pool.pool.Exec(ctx, `INSERT INTO setup_sessions(id,token_digest,created_at,expires_at) VALUES($1,$2,clock_timestamp(),clock_timestamp()+interval '15 minutes')`, persistenceID(233), thirdDigest[:]); err != nil {
		t.Fatal(err)
	}
	got, err := repository.ReserveAuthenticationValidation(ctx, thirdDigest, global, 2)
	if err != nil || got.Outcome != setup.AuthenticationValidationSucceeded {
		t.Fatalf("another session lost global capacity: result=%+v err=%v", got, err)
	}
}

func TestSetupAuthenticationActivationPublicConfidentialAndDraftLock(t *testing.T) {
	for _, tt := range []struct {
		name              string
		clientType        setup.ClientType
		secret            setup.SecretOperation
		wantRuntimeSecret bool
	}{
		{name: "public", clientType: setup.ClientTypePublic, secret: setup.ClearSecret()},
		{name: "confidential", clientType: setup.ClientTypeConfidential, secret: mustReplaceActivationSecret(t), wantRuntimeSecret: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			databaseURL, cleanup := encryptedTestSchema(t)
			defer cleanup()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			pool := migratedEncryptedPool(t, ctx, databaseURL)
			defer pool.Close()
			cipher := secrets.New([32]byte{29})
			keyID := cipher.KeyID()
			secretRepository := NewEncryptedSecretRepository(pool, &keyID)
			if err := secretRepository.EstablishActiveKey(ctx); err != nil {
				t.Fatal(err)
			}
			repository := NewSetupWorkflowRepository(pool, secretRepository, cipher)
			digest := setup.Digest{31: byte(len(tt.name))}
			if _, err := pool.pool.Exec(ctx, `INSERT INTO setup_sessions(id,token_digest,created_at,expires_at) VALUES($1,$2,clock_timestamp(),clock_timestamp()+interval '15 minutes')`, persistenceID(byte(50+len(tt.name))), digest[:]); err != nil {
				t.Fatal(err)
			}
			if _, err := repository.SaveGeneral(ctx, digest, setup.General{InstanceName: "Requestarr", PublicURL: "https://example.com", Timezone: "UTC"}); err != nil {
				t.Fatal(err)
			}
			authentication := setup.Authentication{Issuer: "https://issuer.example/realm", ClientID: "requestarr", ClientType: tt.clientType, Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: "viewer"}
			saved, err := repository.SaveAuthentication(ctx, digest, authentication, tt.secret)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.pool.Exec(ctx, `INSERT INTO setup_authentication_validation_evidence(singleton,authentication_revision,policy_version,metadata_digest,validated_at) VALUES(TRUE,$1,$2,$3,clock_timestamp())`, saved.State.AuthenticationRevision, setup.AuthenticationValidationPolicyVersion, activationBytes(1)); err != nil {
				t.Fatal(err)
			}
			activated, err := repository.ActivateAuthentication(ctx, digest)
			if err != nil || activated.Outcome != setup.ActivationSucceeded || activated.Revision != saved.State.AuthenticationRevision {
				t.Fatalf("activate=%+v err=%v", activated, err)
			}
			var configType setup.ClientType
			if err := pool.pool.QueryRow(ctx, `SELECT client_type FROM auth_runtime_config WHERE singleton=TRUE`).Scan(&configType); err != nil || configType != tt.clientType {
				t.Fatalf("runtime config type=%q err=%v", configType, err)
			}
			_, setupErr := secretRepository.Read(ctx, setupAuthenticationSecretContext)
			_, runtimeErr := secretRepository.Read(ctx, runtimeAuthenticationSecretContext)
			if !errors.Is(setupErr, ErrEncryptedSecretNotFound) || (tt.wantRuntimeSecret != (runtimeErr == nil)) {
				t.Fatalf("setup secret err=%v runtime secret err=%v", setupErr, runtimeErr)
			}
			if _, err := repository.SaveGeneral(ctx, digest, setup.General{InstanceName: "Changed", PublicURL: "https://example.com", Timezone: "UTC"}); err != nil || !errors.Is(err, setup.ErrInvalidPersistenceInput) {
				t.Fatalf("draft save after activation err=%v", err)
			}
			// Reconstruct repository readers to model process restart; persisted group
			// mappings must remain typed and confidential secrets must come only from
			// the runtime source.
			runtime := auth.NewRuntimeConfigurationService(NewAuthRepository(pool), NewRuntimeSecretReader(secretRepository, cipher))
			config, runtimeErr := runtime.VerifyActivated(ctx)
			if runtimeErr != nil || config.AuthenticationRevision != saved.State.AuthenticationRevision || config.DefaultRole != auth.RoleViewer {
				t.Fatalf("runtime restart read=%+v err=%v", config, runtimeErr)
			}
			if tt.clientType == setup.ClientTypePublic {
				if _, err := secretRepository.Read(ctx, runtimeAuthenticationSecretContext); !errors.Is(err, ErrEncryptedSecretNotFound) {
					t.Fatalf("public runtime unexpectedly has secret: %v", err)
				}
			}
		})
	}
}

func mustReplaceActivationSecret(t *testing.T) setup.SecretOperation {
	t.Helper()
	secret, err := setup.ReplaceSecret("activation-secret")
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestSetupAuthenticationActivationRejectsMissingAndStaleEvidenceAtomically(t *testing.T) {
	databaseURL, cleanup := encryptedTestSchema(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := migratedEncryptedPool(t, ctx, databaseURL)
	defer pool.Close()
	cipher := secrets.New([32]byte{31})
	keyID := cipher.KeyID()
	secretRepository := NewEncryptedSecretRepository(pool, &keyID)
	if err := secretRepository.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repository := NewSetupWorkflowRepository(pool, secretRepository, cipher)
	digest := setup.Digest{31: 31}
	if _, err := pool.pool.Exec(ctx, `INSERT INTO setup_sessions(id,token_digest,created_at,expires_at) VALUES($1,$2,clock_timestamp(),clock_timestamp()+interval '15 minutes')`, persistenceID(81), digest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveGeneral(ctx, digest, setup.General{InstanceName: "Requestarr", PublicURL: "https://example.com", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	authentication := setup.Authentication{Issuer: "https://issuer.example/realm", ClientID: "requestarr", ClientType: setup.ClientTypePublic, Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: "viewer"}
	saved, err := repository.SaveAuthentication(ctx, digest, authentication, setup.ClearSecret())
	if err != nil {
		t.Fatal(err)
	}
	missing, err := repository.ActivateAuthentication(ctx, digest)
	if err != nil || missing.Outcome != setup.ActivationRequired {
		t.Fatalf("missing evidence=%+v err=%v", missing, err)
	}
	if _, err := pool.pool.Exec(ctx, `INSERT INTO setup_authentication_validation_evidence(singleton,authentication_revision,policy_version,metadata_digest,validated_at) VALUES(TRUE,$1,$2,$3,clock_timestamp())`, saved.State.AuthenticationRevision+1, setup.AuthenticationValidationPolicyVersion, activationBytes(3)); err != nil {
		t.Fatal(err)
	}
	stale, err := repository.ActivateAuthentication(ctx, digest)
	if err != nil || stale.Outcome != setup.ActivationStale {
		t.Fatalf("stale evidence=%+v err=%v", stale, err)
	}
	for _, query := range []string{
		`SELECT count(*) FROM auth_runtime_config`,
		`SELECT count(*) FROM auth_runtime_activation`,
		`SELECT count(*) FROM setup_workflow_events WHERE event_type='authentication_activated'`,
		`SELECT count(*) FROM auth_audit_events WHERE event_type='configuration_activated'`,
	} {
		var count int
		if err := pool.pool.QueryRow(ctx, query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("atomicity query=%q count=%d err=%v", query, count, err)
		}
	}
}

func TestSetupAuthenticationActivationConcurrentIdempotency(t *testing.T) {
	databaseURL, cleanup := encryptedTestSchema(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := migratedEncryptedPool(t, ctx, databaseURL)
	defer pool.Close()
	cipher := secrets.New([32]byte{37})
	keyID := cipher.KeyID()
	secretRepository := NewEncryptedSecretRepository(pool, &keyID)
	if err := secretRepository.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repository := NewSetupWorkflowRepository(pool, secretRepository, cipher)
	digest := setup.Digest{31: 37}
	if _, err := pool.pool.Exec(ctx, `INSERT INTO setup_sessions(id,token_digest,created_at,expires_at) VALUES($1,$2,clock_timestamp(),clock_timestamp()+interval '15 minutes')`, persistenceID(87), digest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SaveGeneral(ctx, digest, setup.General{InstanceName: "Requestarr", PublicURL: "https://example.com", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	saved, err := repository.SaveAuthentication(ctx, digest, setup.Authentication{Issuer: "https://issuer.example/realm", ClientID: "requestarr", ClientType: setup.ClientTypePublic, Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: "viewer"}, setup.ClearSecret())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.pool.Exec(ctx, `INSERT INTO setup_authentication_validation_evidence(singleton,authentication_revision,policy_version,metadata_digest,validated_at) VALUES(TRUE,$1,$2,$3,clock_timestamp())`, saved.State.AuthenticationRevision, setup.AuthenticationValidationPolicyVersion, activationBytes(4)); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan setup.ActivationResult, 2)
	errs := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, activateErr := repository.ActivateAuthentication(ctx, digest)
			results <- result
			errs <- activateErr
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errs)
	var succeeded, alreadyActive int
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range results {
		if result.Outcome == setup.ActivationSucceeded {
			succeeded++
		}
		if result.Outcome == setup.ActivationAlreadyActive {
			alreadyActive++
		}
	}
	if succeeded != 1 || alreadyActive != 1 {
		t.Fatalf("succeeded=%d alreadyActive=%d", succeeded, alreadyActive)
	}
	var events int
	if err := pool.pool.QueryRow(ctx, `SELECT count(*) FROM setup_workflow_events WHERE event_type='authentication_activated'`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("activation events=%d err=%v", events, err)
	}
}

func activationBytes(value byte) []byte { return []byte(strings.Repeat(string([]byte{value}), 32)) }
