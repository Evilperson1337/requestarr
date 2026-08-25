//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
	"git.example.com/evilperson1337/Requestarr/internal/secrets"
	"git.example.com/evilperson1337/Requestarr/internal/store/postgres/migrations"
)

func TestSourceConfigurationRepositoryCASIsolationAndRollback(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{21})
	keyID := cipher.KeyID()
	secretRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo)
	first := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000201", "00000000-0000-4000-8000-000000000202", 0)
	result, err := repo.SaveSource(ctx, first)
	if err != nil || result.Revision != 1 {
		t.Fatalf("create=%+v err=%v", result, err)
	}
	// Replaying the durable audit ID is idempotent; it must not create audit rows.
	replayed, err := repo.SaveSource(ctx, first)
	if err != nil || replayed.Revision != 1 || !replayed.AuditReplayed {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	// AES-GCM envelopes use fresh nonces. An independently encrypted equivalent
	// credential must still be the same audit command.
	independentlyEncrypted := first
	owner, err := sourceSecretOwner(first.Configuration.ID)
	if err != nil {
		t.Fatal(err)
	}
	independentlyEncrypted.Credential, err = cipher.Encrypt(secrets.Context{Namespace: inventory.InventoryCredentialNamespace, OwnerID: owner, Name: inventory.InventoryCredentialName}, []byte("credential"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveSource(ctx, independentlyEncrypted); err != nil {
		t.Fatalf("independently encrypted replay=%v", err)
	}
	changedCredential := first
	changedCredential.Credential, err = cipher.Encrypt(secrets.Context{Namespace: inventory.InventoryCredentialNamespace, OwnerID: owner, Name: inventory.InventoryCredentialName}, []byte("changed credential"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveSource(ctx, changedCredential); !errors.Is(err, inventory.ErrSourceSaveReplayConflict) {
		t.Fatalf("changed credential replay=%v", err)
	}
	mismatchedReplay := first
	mismatchedReplay.Configuration.Name = "Different Books"
	if _, err := repo.SaveSource(ctx, mismatchedReplay); !errors.Is(err, inventory.ErrSourceSaveReplayConflict) {
		t.Fatalf("mismatched replay=%v", err)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inventory_source_configuration_audit`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audits=%d err=%v", audits, err)
	}

	stale := sourceConfigurationSave(t, cipher, first.Configuration.ID, "00000000-0000-4000-8000-000000000203", 0)
	if _, err := repo.SaveSource(ctx, stale); !errors.Is(err, inventory.ErrSourceRevisionConflict) {
		t.Fatalf("stale update=%v", err)
	}
	updated := sourceConfigurationSave(t, cipher, first.Configuration.ID, "00000000-0000-4000-8000-000000000204", 1)
	updated.Configuration.SelectedLibraries = []string{" nonfiction ", "fiction"}
	result, err = repo.SaveSource(ctx, updated)
	if err != nil || result.Revision != 2 {
		t.Fatalf("update=%+v err=%v", result, err)
	}
	var auditLibraries []string
	if err := pool.QueryRow(ctx, `SELECT array_agg(remote_library_id ORDER BY remote_library_id) FROM inventory_source_configuration_audit_selected_libraries WHERE audit_id=$1`, updated.AuditID).Scan(&auditLibraries); err != nil || len(auditLibraries) != 2 || auditLibraries[0] != "fiction" || auditLibraries[1] != "nonfiction" {
		t.Fatalf("canonical audit libraries=%q err=%v", auditLibraries, err)
	}
	if _, err := repo.SaveSource(ctx, updated); err != nil {
		t.Fatalf("updated replay=%v", err)
	}
	var auditLibraryCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inventory_source_configuration_audit_selected_libraries WHERE audit_id=$1`, updated.AuditID).Scan(&auditLibraryCount); err != nil || auditLibraryCount != 2 {
		t.Fatalf("replayed audit libraries=%d err=%v", auditLibraryCount, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_source_configuration_audit_selected_libraries SET remote_library_id='changed' WHERE audit_id=$1`, updated.AuditID); err == nil {
		t.Fatal("direct audit library update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM inventory_source_configuration_audit_selected_libraries WHERE audit_id=$1`, updated.AuditID); err == nil {
		t.Fatal("direct audit library delete unexpectedly succeeded")
	}
	var revision, libraries int
	if err := pool.QueryRow(ctx, `SELECT revision FROM inventory_source_configurations WHERE source_id=$1`, first.Configuration.ID).Scan(&revision); err != nil || revision != 2 {
		t.Fatalf("revision=%d err=%v", revision, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inventory_source_selected_libraries WHERE source_id=$1`, first.Configuration.ID).Scan(&libraries); err != nil || libraries != 2 {
		t.Fatalf("libraries=%d err=%v", libraries, err)
	}

	var secretsCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM encrypted_secrets WHERE namespace='inventory-source'`).Scan(&secretsCount); err != nil || secretsCount != 1 {
		t.Fatalf("secret isolation=%d err=%v", secretsCount, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM encrypted_secrets WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential'`, first.Configuration.ID); err == nil {
		t.Fatal("direct configured credential delete unexpectedly succeeded")
	}
}

func TestSourceConfigurationRepositoryPlexRevisionEvidence(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{31})
	keyID := cipher.KeyID()
	secretRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo)
	first := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000281", "00000000-0000-4000-8000-000000000282", 0)
	first.Configuration = inventory.SourceConfiguration{ID: first.Configuration.ID, Name: "Plex", Kind: inventory.SourcePlex, Enabled: true, MediaTypes: []domain.MediaType{domain.MediaMovie}, SelectedLibraries: []string{"1"}, Capabilities: inventory.SourceCapabilities{CompleteEnumeration: true}}
	first.EndpointURL = "https://plex.example.test"
	if result, err := repo.SaveSource(ctx, first); err != nil || result.Revision != 1 {
		t.Fatalf("Plex create=%+v err=%v", result, err)
	}
	if result, err := repo.SaveSource(ctx, first); err != nil || !result.AuditReplayed || result.Revision != 1 {
		t.Fatalf("Plex replay=%+v err=%v", result, err)
	}
	stale := first
	stale.AuditID = "00000000-0000-4000-8000-000000000283"
	if _, err := repo.SaveSource(ctx, stale); !errors.Is(err, inventory.ErrSourceRevisionConflict) {
		t.Fatalf("Plex stale CAS=%v", err)
	}
	updated := first
	updated.ExpectedRevision, updated.AuditID = 1, "00000000-0000-4000-8000-000000000284"
	updated.Configuration.Name = "Updated Plex"
	updated.Configuration.SelectedLibraries = []string{"2", "1"}
	if result, err := repo.SaveSource(ctx, updated); err != nil || result.Revision != 2 {
		t.Fatalf("Plex update=%+v err=%v", result, err)
	}
	var selectedLibraries []string
	if err := pool.QueryRow(ctx, `SELECT array_agg(remote_library_id ORDER BY remote_library_id) FROM inventory_source_selected_libraries WHERE source_id=$1`, first.Configuration.ID).Scan(&selectedLibraries); err != nil || len(selectedLibraries) != 2 || selectedLibraries[0] != "1" || selectedLibraries[1] != "2" {
		t.Fatalf("nonempty library selection replacement = %v with err %v, want [1 2]", selectedLibraries, err)
	}
	var revisions, scopes int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM inventory_source_configuration_revisions WHERE source_id=$1), (SELECT count(*) FROM inventory_source_configuration_revision_selected_libraries WHERE source_id=$1)`, first.Configuration.ID).Scan(&revisions, &scopes); err != nil || revisions != 2 || scopes != 3 {
		t.Fatalf("Plex revision evidence=%d/%d err=%v", revisions, scopes, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_source_configuration_revisions SET source_name='changed' WHERE source_id=$1 AND revision=1`, first.Configuration.ID); err == nil {
		t.Fatal("direct revision mutation unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM inventory_source_configuration_revision_selected_libraries WHERE source_id=$1 AND revision=1`, first.Configuration.ID); err == nil {
		t.Fatal("direct revision scope delete unexpectedly succeeded")
	}
	view, err := repo.GetSourceConfiguration(ctx, first.Configuration.ID)
	if err != nil || view.Configuration.Kind != inventory.SourcePlex || view.Revision != 2 || len(view.Configuration.SelectedLibraries) != 2 {
		t.Fatalf("Plex view=%+v err=%v", view, err)
	}
}

func TestSourceConfigurationRepositoryRollsBackAuditLibraryEvidence(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{24})
	keyID := cipher.KeyID()
	secretRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo)
	save := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000241", "00000000-0000-4000-8000-000000000242", 0)
	save.Configuration.SelectedLibraries = []string{"rollback"}
	if _, err := pool.Exec(ctx, `ALTER TABLE inventory_source_configuration_audit_selected_libraries ADD CONSTRAINT test_reject_audit_library CHECK (remote_library_id <> 'rollback')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveSource(ctx, save); err == nil {
		t.Fatal("save with rejected audit library unexpectedly succeeded")
	}
	var sources, configurations, libraries, secretsCount, audits, auditLibraries, intents int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM library_sources WHERE id=$1),
		(SELECT count(*) FROM inventory_source_configurations WHERE source_id=$1),
		(SELECT count(*) FROM inventory_source_selected_libraries WHERE source_id=$1),
		(SELECT count(*) FROM encrypted_secrets WHERE namespace='inventory-source'),
		(SELECT count(*) FROM inventory_source_configuration_audit WHERE id=$2),
		(SELECT count(*) FROM inventory_source_configuration_audit_selected_libraries WHERE audit_id=$2),
		(SELECT count(*) FROM inventory_manual_sync_intents WHERE source_id=$1)`, save.Configuration.ID, save.AuditID).Scan(&sources, &configurations, &libraries, &secretsCount, &audits, &auditLibraries, &intents); err != nil {
		t.Fatal(err)
	}
	if sources != 0 || configurations != 0 || libraries != 0 || secretsCount != 0 || audits != 0 || auditLibraries != 0 || intents != 0 {
		t.Fatalf("rollback state sources/configuration/libraries/secrets/audits/auditLibraries/intents=%d/%d/%d/%d/%d/%d/%d", sources, configurations, libraries, secretsCount, audits, auditLibraries, intents)
	}
}

func TestSourceConfigurationRepositorySecretFreeViewsAndCredentialOperations(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{25})
	keyID := cipher.KeyID()
	secretsRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretsRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretsRepo)
	seed := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000251", "00000000-0000-4000-8000-000000000252", 0)
	if _, err := repo.SaveSource(ctx, seed); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_source_status SET state='failed',last_started_at=clock_timestamp(),last_completed_at=clock_timestamp(),failure_detail='source unavailable' WHERE source_id=$1`, seed.Configuration.ID); err != nil {
		t.Fatal(err)
	}
	view, err := repo.GetSourceConfiguration(ctx, seed.Configuration.ID)
	if err != nil || !view.CredentialConfigured || view.Revision != 1 || view.Status.State != inventory.SourceSyncFailed || view.Status.FailureDetail != string(inventory.SourceFailureUnavailable) {
		t.Fatalf("get secret-free view=%+v err=%v", view, err)
	}
	views, err := repo.ListSourceConfigurations(ctx)
	if err != nil || len(views) != 1 || views[0].Configuration.ID != seed.Configuration.ID {
		t.Fatalf("list secret-free views=%+v err=%v", views, err)
	}
	if _, err := repo.GetSourceConfiguration(ctx, "00000000-0000-4000-8000-000000000253"); !errors.Is(err, inventory.ErrSourceNotFound) {
		t.Fatalf("get missing=%v", err)
	}

	var beforeCiphertext, beforeIdentity []byte
	if err := pool.QueryRow(ctx, `SELECT ciphertext,credential_identity FROM encrypted_secrets WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential'`, seed.Configuration.ID).Scan(&beforeCiphertext, &beforeIdentity); err != nil {
		t.Fatal(err)
	}
	preserve := inventory.SourceConfigurationSave{Configuration: seed.Configuration, EndpointURL: seed.EndpointURL, Credential: inventory.SourceCredentialChange{Preserve: true}, ExpectedRevision: 1, AuditID: "00000000-0000-4000-8000-000000000254", OccurredAt: time.Now().UTC()}
	preserve.Configuration.Name = "Preserved Books"
	preserve.Configuration.SelectedLibraries = []string{"preserved"}
	if result, err := repo.SaveSourceConfiguration(ctx, preserve); err != nil || result.Revision != 2 {
		t.Fatalf("preserve=%+v err=%v", result, err)
	}
	var preservedCiphertext, preservedIdentity []byte
	if err := pool.QueryRow(ctx, `SELECT ciphertext,credential_identity FROM encrypted_secrets WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential'`, seed.Configuration.ID).Scan(&preservedCiphertext, &preservedIdentity); err != nil {
		t.Fatal(err)
	}
	if string(beforeCiphertext) != string(preservedCiphertext) || string(beforeIdentity) != string(preservedIdentity) {
		t.Fatal("preserve changed encrypted credential")
	}
	var auditCountBeforeReplay int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inventory_source_configuration_audit WHERE source_id=$1`, seed.Configuration.ID).Scan(&auditCountBeforeReplay); err != nil {
		t.Fatal(err)
	}
	result, err := repo.SaveSourceConfiguration(ctx, preserve)
	if err != nil || !result.AuditReplayed || result.Revision != 2 {
		t.Fatalf("preserve replay=%+v err=%v", result, err)
	}
	view, err = repo.GetSourceConfiguration(ctx, seed.Configuration.ID)
	if err != nil || view.Revision != 2 || len(view.Configuration.SelectedLibraries) != 1 || view.Configuration.SelectedLibraries[0] != "preserved" {
		t.Fatalf("preserve replay view=%+v err=%v", view, err)
	}
	var auditCountAfterReplay int
	var replayedCiphertext, replayedIdentity []byte
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM inventory_source_configuration_audit WHERE source_id=$1),
		(SELECT ciphertext FROM encrypted_secrets WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential'),
		(SELECT credential_identity FROM encrypted_secrets WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential')`, seed.Configuration.ID).Scan(&auditCountAfterReplay, &replayedCiphertext, &replayedIdentity); err != nil {
		t.Fatal(err)
	}
	if auditCountAfterReplay != auditCountBeforeReplay || string(replayedCiphertext) != string(preservedCiphertext) || string(replayedIdentity) != string(preservedIdentity) {
		t.Fatalf("preserve replay mutated audit or credential: audits=%d/%d ciphertextChanged=%t identityChanged=%t", auditCountBeforeReplay, auditCountAfterReplay, string(replayedCiphertext) != string(preservedCiphertext), string(replayedIdentity) != string(preservedIdentity))
	}

	owner, err := sourceSecretOwner(seed.Configuration.ID)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := cipher.Encrypt(secrets.Context{Namespace: inventory.InventoryCredentialNamespace, OwnerID: owner, Name: inventory.InventoryCredentialName}, []byte("replacement"))
	if err != nil {
		t.Fatal(err)
	}
	replace := preserve
	replace.ExpectedRevision, replace.AuditID = 2, "00000000-0000-4000-8000-000000000255"
	replace.Credential = inventory.SourceCredentialChange{Replace: replacement}
	if result, err := repo.SaveSourceConfiguration(ctx, replace); err != nil || result.Revision != 3 {
		t.Fatalf("replace=%+v err=%v", result, err)
	}
	var replacedIdentity []byte
	if err := pool.QueryRow(ctx, `SELECT credential_identity FROM encrypted_secrets WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential'`, seed.Configuration.ID).Scan(&replacedIdentity); err != nil {
		t.Fatal(err)
	}
	if string(replacedIdentity) == string(beforeIdentity) {
		t.Fatal("replace retained credential identity")
	}
}

func TestSourceConfigurationRepositoryPreserveRollbackIsAtomic(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{26})
	keyID := cipher.KeyID()
	secretsRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretsRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretsRepo)
	seed := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000261", "00000000-0000-4000-8000-000000000262", 0)
	if _, err := repo.SaveSource(ctx, seed); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE inventory_source_configuration_audit_selected_libraries ADD CONSTRAINT test_reject_preserve_audit CHECK (remote_library_id <> 'reject-preserve')`); err != nil {
		t.Fatal(err)
	}
	preserve := inventory.SourceConfigurationSave{Configuration: seed.Configuration, EndpointURL: "https://changed.example.test", Credential: inventory.SourceCredentialChange{Preserve: true}, ExpectedRevision: 1, AuditID: "00000000-0000-4000-8000-000000000263", OccurredAt: time.Now().UTC()}
	preserve.Configuration.SelectedLibraries = []string{"reject-preserve"}
	if _, err := repo.SaveSourceConfiguration(ctx, preserve); err == nil {
		t.Fatal("rejected preserve update unexpectedly succeeded")
	}
	view, err := repo.GetSourceConfiguration(ctx, seed.Configuration.ID)
	if err != nil || view.Revision != 1 || view.EndpointURL != seed.EndpointURL || len(view.Configuration.SelectedLibraries) != 1 || view.Configuration.SelectedLibraries[0] != "fiction" {
		t.Fatalf("rollback view=%+v err=%v", view, err)
	}
}

func TestConfiguredAudiobookshelfDirectSQLInvariant(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-4000-8000-000000000231"
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,enabled,authoritative_media_types,created_at,updated_at) VALUES($1,'audiobookshelf','Books',NULL,true,'["audiobook"]',$2,$2)`, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_source_configurations(source_id,endpoint_url,updated_at) VALUES($1,'https://books.example.test',$2)`, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE library_sources SET base_url='https://legacy.example.test' WHERE id=$1`, id); err == nil {
		t.Fatal("direct legacy base_url update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `UPDATE library_sources SET secret_ref='legacy' WHERE id=$1`, id); err == nil {
		t.Fatal("direct legacy secret_ref update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `UPDATE library_sources SET kind='plex' WHERE id=$1`, id); err == nil {
		t.Fatal("direct source retype unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM inventory_source_configurations WHERE source_id=$1`, id); err == nil {
		t.Fatal("direct configuration delete unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM library_sources WHERE id=$1`, id); err == nil {
		t.Fatal("direct source delete unexpectedly succeeded")
	}
}

func TestConfiguredSourceDirectSQLProjectionIntegrity(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{41})
	keyID := cipher.KeyID()
	secretRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo)
	save := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000291", "00000000-0000-4000-8000-000000000292", 0)
	if _, err := repo.SaveSource(ctx, save); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		`UPDATE library_sources SET name='changed' WHERE id=$1`,
		`UPDATE library_sources SET enabled=false WHERE id=$1`,
		`UPDATE library_sources SET authoritative_media_types='["movie"]' WHERE id=$1`,
		`UPDATE inventory_source_configurations SET endpoint_url='https://changed.example.test' WHERE source_id=$1`,
		`UPDATE inventory_source_configurations SET revision=2 WHERE source_id=$1`,
		`DELETE FROM inventory_source_selected_libraries WHERE source_id=$1`,
		`UPDATE encrypted_secrets SET envelope_version=2 WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential'`,
		`UPDATE encrypted_secrets SET key_id=decode(repeat('ff',16),'hex') WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential'`,
		`UPDATE encrypted_secrets SET nonce=decode(repeat('ff',12),'hex') WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential'`,
		`UPDATE encrypted_secrets SET ciphertext=decode(repeat('ff',16),'hex') WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential'`,
		`UPDATE encrypted_secrets SET credential_identity=decode(repeat('ff',32),'hex') WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential'`,
	} {
		if _, err := pool.Exec(ctx, mutation, save.Configuration.ID); err == nil {
			t.Fatalf("direct projection mutation unexpectedly succeeded: %s", mutation)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE library_sources SET kind='plex' WHERE id=$1`, save.Configuration.ID); err == nil {
		t.Fatal("configured source retype unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `UPDATE library_sources SET authoritative_media_types='["audiobook","movie"]' WHERE id=$1`, save.Configuration.ID); err == nil {
		t.Fatal("configured Audiobookshelf media mismatch unexpectedly succeeded")
	}
}

func TestConfiguredPlexRejectsInvalidMediaTypeShapes(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{43})
	keyID := cipher.KeyID()
	secretRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo)
	save := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000296", "00000000-0000-4000-8000-000000000297", 0)
	save.Configuration.Kind = inventory.SourcePlex
	save.Configuration.MediaTypes = []domain.MediaType{domain.MediaMovie}
	save.EndpointURL = "https://plex.example.test"
	if _, err := repo.SaveSource(ctx, save); err != nil {
		t.Fatal(err)
	}
	for _, mediaTypes := range []string{
		`[]`,
		`["movie","movie"]`,
		`["audiobook"]`,
		`["movie","tv","album","song","audiobook"]`,
		`{"movie":true}`,
	} {
		if _, err := pool.Exec(ctx, `UPDATE library_sources SET authoritative_media_types=$2::jsonb WHERE id=$1`, save.Configuration.ID, mediaTypes); err == nil {
			t.Fatalf("invalid Plex media type shape unexpectedly succeeded: %s", mediaTypes)
		}
	}
}

func TestConfiguredSourceRevisionEvidenceDirectSQLIntegrity(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{42})
	keyID := cipher.KeyID()
	secretRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo)
	save := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000293", "00000000-0000-4000-8000-000000000294", 0)
	if _, err := repo.SaveSource(ctx, save); err != nil {
		t.Fatal(err)
	}
	owner, err := sourceSecretOwner(save.Configuration.ID)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := cipher.Encrypt(secrets.Context{Namespace: inventory.InventoryCredentialNamespace, OwnerID: owner, Name: inventory.InventoryCredentialName}, []byte("different"))
	if err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO inventory_source_configuration_audit(id,source_id,action,configuration_revision,command_fingerprint,credential_identity,occurred_at) VALUES('00000000-0000-4000-8000-000000000295',$1,'updated',2,decode(repeat('01',32),'hex'),decode(repeat('02',32),'hex'),clock_timestamp())`, []any{save.Configuration.ID}},
		{`INSERT INTO inventory_source_configuration_revisions(source_id,revision,source_kind,source_name,endpoint_url,enabled,enabled_media_types,credential_identity,audit_id,recorded_at) VALUES($1,2,'audiobookshelf','Books','https://books.example.test',true,'["audiobook"]',decode(repeat('03',32),'hex'),'00000000-0000-4000-8000-000000000294',clock_timestamp())`, []any{save.Configuration.ID}},
		{`INSERT INTO inventory_source_configuration_revision_selected_libraries(source_id,revision,remote_library_id) VALUES($1,1,'different')`, []any{save.Configuration.ID}},
		{`UPDATE encrypted_secrets SET ciphertext=$2,credential_identity=$3 WHERE namespace='inventory-source' AND owner_id=$1::uuid AND secret_name='credential'`, []any{save.Configuration.ID, envelope.Ciphertext, envelope.Identity[:]}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err == nil {
			t.Fatalf("mismatched revision evidence mutation unexpectedly succeeded: %s", statement.query)
		}
	}
}

func TestConfiguredAudiobookshelfDirectSQLRaceIsSerialized(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-4000-8000-000000000232"
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,enabled,authoritative_media_types,created_at,updated_at) VALUES($1,'audiobookshelf','Books',NULL,true,'["audiobook"]',$2,$2)`, id, now); err != nil {
		t.Fatal(err)
	}
	configurationTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer configurationTx.Rollback(ctx)
	if _, err := configurationTx.Exec(ctx, `INSERT INTO inventory_source_configurations(source_id,endpoint_url,updated_at) VALUES($1,'https://books.example.test',$2)`, id, now); err != nil {
		t.Fatal(err)
	}
	updateStarted := make(chan struct{})
	updateDone := make(chan error, 1)
	go func() {
		legacyTx, err := pool.Begin(ctx)
		if err != nil {
			updateDone <- err
			return
		}
		defer legacyTx.Rollback(ctx)
		close(updateStarted)
		_, err = legacyTx.Exec(ctx, `UPDATE library_sources SET base_url='https://legacy.example.test' WHERE id=$1`, id)
		updateDone <- err
	}()
	<-updateStarted
	select {
	case err := <-updateDone:
		t.Fatalf("legacy update bypassed source lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := configurationTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-updateDone; err == nil {
		t.Fatal("serialized legacy update unexpectedly succeeded")
	}
}

func TestConfiguredAudiobookshelfDirectSQLDeleteRaceIsRetained(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-4000-8000-000000000233"
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,enabled,authoritative_media_types,created_at,updated_at) VALUES($1,'audiobookshelf','Books',NULL,true,'["audiobook"]',$2,$2)`, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_source_configurations(source_id,endpoint_url,updated_at) VALUES($1,'https://books.example.test',$2)`, id, now); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, statement := range []string{
		`DELETE FROM inventory_source_configurations WHERE source_id=$1`,
		`DELETE FROM library_sources WHERE id=$1`,
	} {
		wg.Add(1)
		go func(statement string) {
			defer wg.Done()
			_, err := pool.Exec(ctx, statement, id)
			errs <- err
		}(statement)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err == nil {
			t.Fatal("concurrent direct delete unexpectedly succeeded")
		}
	}
}

func TestSourceConfigurationRepositoryRejectsLegacyAndRunningSource(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{22})
	keyID := cipher.KeyID()
	secretRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo)
	legacyID := "00000000-0000-4000-8000-000000000211"
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,secret_ref,authoritative_media_types,created_at,updated_at) VALUES($1,'plex','Legacy','https://plex.example','old','["movie"]',$2,$2)`, legacyID, now); err != nil {
		t.Fatal(err)
	}
	legacy := sourceConfigurationSave(t, cipher, legacyID, "00000000-0000-4000-8000-000000000212", 0)
	if _, err := repo.SaveSource(ctx, legacy); !errors.Is(err, ErrInventorySourceLegacy) {
		t.Fatalf("legacy repurpose=%v", err)
	}

	configured := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000213", "00000000-0000-4000-8000-000000000214", 0)
	if _, err := repo.SaveSource(ctx, configured); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_source_status SET state='running',last_started_at=$2 WHERE source_id=$1`, configured.Configuration.ID, now); err != nil {
		t.Fatal(err)
	}
	blocked := sourceConfigurationSave(t, cipher, configured.Configuration.ID, "00000000-0000-4000-8000-000000000215", 1)
	if _, err := repo.SaveSource(ctx, blocked); !errors.Is(err, inventory.ErrSourceReconfigurationBusy) {
		t.Fatalf("running reconfiguration=%v", err)
	}
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT revision FROM inventory_source_configurations WHERE source_id=$1`, configured.Configuration.ID).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("running revision=%d err=%v", revision, err)
	}
}

func TestSourceConfigurationRepositoryConcurrentCAS(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{23})
	keyID := cipher.KeyID()
	secretRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo)
	seed := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000221", "00000000-0000-4000-8000-000000000222", 0)
	if _, err := repo.SaveSource(ctx, seed); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, auditID := range []string{"00000000-0000-4000-8000-000000000223", "00000000-0000-4000-8000-000000000224"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			save := sourceConfigurationSave(t, cipher, seed.Configuration.ID, id, 1)
			_, err := repo.SaveSource(ctx, save)
			errs <- err
		}(auditID)
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, inventory.ErrSourceRevisionConflict) {
			t.Fatalf("concurrent save=%v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful writes=%d", successes)
	}
}

func sourceConfigurationSave(t *testing.T, cipher *secrets.Cipher, sourceID, auditID string, revision int64) inventory.SourceSave {
	t.Helper()
	owner, err := sourceSecretOwner(sourceID)
	if err != nil {
		t.Fatal(err)
	}
	semantic := secrets.Context{Namespace: inventory.InventoryCredentialNamespace, OwnerID: owner, Name: inventory.InventoryCredentialName}
	envelope, err := cipher.Encrypt(semantic, []byte("credential"))
	if err != nil {
		t.Fatal(err)
	}
	return inventory.SourceSave{Configuration: inventory.SourceConfiguration{ID: sourceID, Name: "Books", Kind: inventory.SourceAudiobookshelf, Enabled: true, MediaTypes: []domain.MediaType{domain.MediaAudiobook}, SelectedLibraries: []string{"fiction"}, Capabilities: inventory.SourceCapabilities{CompleteEnumeration: true}}, EndpointURL: "https://books.example.test", Credential: envelope, ExpectedRevision: revision, AuditID: auditID, OccurredAt: time.Now().UTC()}
}
