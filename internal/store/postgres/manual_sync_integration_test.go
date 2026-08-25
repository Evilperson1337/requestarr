//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"git.example.com/evilperson1337/Requestarr/internal/inventory"
	"git.example.com/evilperson1337/Requestarr/internal/secrets"
	"git.example.com/evilperson1337/Requestarr/internal/store/postgres/migrations"
)

func TestManualSyncRepositoryRecordsReplayCoalescesAndLeavesConfigurationUntouched(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{25})
	keyID := cipher.KeyID()
	secretRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	sourceRepo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo)
	source := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000251", "00000000-0000-4000-8000-000000000252", 0)
	if _, err := sourceRepo.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	repo := NewManualSyncRepository(&Pool{pool: pool})
	requestedAt := time.Now().UTC().Round(0)
	request := inventory.ManualSyncRequest{ID: "00000000-0000-4000-8000-000000000253", SourceID: source.Configuration.ID, RequestedAt: requestedAt}
	result, err := repo.RequestManualSync(ctx, request)
	if err != nil || result.Disposition != inventory.ManualSyncRecorded || result.PendingIntentID != request.ID {
		t.Fatalf("record=%+v err=%v", result, err)
	}
	replayed, err := repo.RequestManualSync(ctx, request)
	if err != nil || replayed.Disposition != inventory.ManualSyncReplayed || replayed.PendingIntentID != request.ID {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	mismatch := request
	mismatch.RequestedAt = mismatch.RequestedAt.Add(time.Second)
	if _, err := repo.RequestManualSync(ctx, mismatch); !errors.Is(err, inventory.ErrManualSyncRequestConflict) {
		t.Fatalf("mismatched replay=%v", err)
	}
	coalescedRequest := inventory.ManualSyncRequest{ID: "00000000-0000-4000-8000-000000000254", SourceID: source.Configuration.ID, RequestedAt: requestedAt.Add(time.Second)}
	coalesced, err := repo.RequestManualSync(ctx, coalescedRequest)
	if err != nil || coalesced.Disposition != inventory.ManualSyncCoalesced || coalesced.PendingIntentID != request.ID {
		t.Fatalf("coalesced=%+v err=%v", coalesced, err)
	}
	var revision, configurationAudits, selectedLibraries, credentials, intents, attempts int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT revision FROM inventory_source_configurations WHERE source_id=$1),
		(SELECT count(*) FROM inventory_source_configuration_audit WHERE source_id=$1),
		(SELECT count(*) FROM inventory_source_selected_libraries WHERE source_id=$1),
		(SELECT count(*) FROM encrypted_secrets WHERE namespace='inventory-source' AND owner_id=$1::uuid),
		(SELECT count(*) FROM inventory_manual_sync_intents WHERE source_id=$1),
		(SELECT count(*) FROM inventory_manual_sync_request_attempts WHERE source_id=$1)`, source.Configuration.ID).Scan(&revision, &configurationAudits, &selectedLibraries, &credentials, &intents, &attempts); err != nil {
		t.Fatal(err)
	}
	if revision != 1 || configurationAudits != 1 || selectedLibraries != 1 || credentials != 1 || intents != 1 || attempts != 3 {
		t.Fatalf("state revision/audits/libraries/credentials/intents/attempts=%d/%d/%d/%d/%d/%d", revision, configurationAudits, selectedLibraries, credentials, intents, attempts)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_manual_sync_request_attempts SET disposition='recorded' WHERE request_id=$1`, request.ID); err == nil {
		t.Fatal("direct request attempt mutation unexpectedly succeeded")
	}
}

func TestManualSyncRepositoryRollsBackFailedEvidenceWrite(t *testing.T) {
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
	source := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000261", "00000000-0000-4000-8000-000000000262", 0)
	if _, err := NewSourceConfigurationRepository(&Pool{pool: pool}, secretsRepo).SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE inventory_manual_sync_request_attempts ADD CONSTRAINT reject_recorded_attempt CHECK (disposition <> 'recorded')`); err != nil {
		t.Fatal(err)
	}
	request := inventory.ManualSyncRequest{ID: "00000000-0000-4000-8000-000000000263", SourceID: source.Configuration.ID, RequestedAt: time.Now().UTC()}
	if _, err := NewManualSyncRepository(&Pool{pool: pool}).RequestManualSync(ctx, request); err == nil {
		t.Fatal("request unexpectedly succeeded")
	}
	var intents, attempts int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM inventory_manual_sync_intents WHERE source_id=$1),(SELECT count(*) FROM inventory_manual_sync_request_attempts WHERE request_id=$2)`, request.SourceID, request.ID).Scan(&intents, &attempts); err != nil {
		t.Fatal(err)
	}
	if intents != 0 || attempts != 0 {
		t.Fatalf("rollback intents/attempts=%d/%d", intents, attempts)
	}
}

func TestManualSyncRepositoryDoesNotClassifyActorForeignKeyViolationAsSourceNotFound(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{29})
	keyID := cipher.KeyID()
	secretsRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretsRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	source := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000296", "00000000-0000-4000-8000-000000000297", 0)
	if _, err := NewSourceConfigurationRepository(&Pool{pool: pool}, secretsRepo).SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	request := inventory.ManualSyncRequest{
		ID:          "00000000-0000-4000-8000-000000000298",
		SourceID:    source.Configuration.ID,
		RequestedBy: "00000000-0000-4000-8000-000000000299",
		RequestedAt: time.Now().UTC(),
	}
	_, err := NewManualSyncRepository(&Pool{pool: pool}).RequestManualSync(ctx, request)
	if err == nil {
		t.Fatal("request unexpectedly succeeded")
	}
	if errors.Is(err, ErrInventorySourceNotFound) || errors.Is(err, inventory.ErrManualSyncSourceNotFound) {
		t.Fatalf("actor foreign-key violation classified as source not found: %v", err)
	}
	var intents, attempts int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM inventory_manual_sync_intents WHERE source_id=$1),
		(SELECT count(*) FROM inventory_manual_sync_request_attempts WHERE request_id=$2)`, request.SourceID, request.ID).Scan(&intents, &attempts); err != nil {
		t.Fatal(err)
	}
	if intents != 0 || attempts != 0 {
		t.Fatalf("foreign-key failure was not atomic: intents/attempts=%d/%d", intents, attempts)
	}
}

func TestManualSyncRepositoryClassifiesIneligibleSources(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,enabled,authoritative_media_types,created_at,updated_at) VALUES
		('00000000-0000-4000-8000-000000000281','audiobookshelf','Unconfigured',true,'["audiobook"]',$1,$1),
		('00000000-0000-4000-8000-000000000282','plex','Wrong kind',true,'["movie"]',$1,$1),
		('00000000-0000-4000-8000-000000000283','audiobookshelf','Disabled',false,'["audiobook"]',$1,$1);
		INSERT INTO inventory_source_configurations(source_id,endpoint_url,updated_at) VALUES ('00000000-0000-4000-8000-000000000283','https://books.example.test',$1)`, now); err != nil {
		t.Fatal(err)
	}
	repo := NewManualSyncRepository(&Pool{pool: pool})
	for _, test := range []struct {
		name, sourceID string
		want           error
	}{
		{name: "missing", sourceID: "00000000-0000-4000-8000-000000000284", want: inventory.ErrManualSyncSourceNotFound},
		{name: "unconfigured", sourceID: "00000000-0000-4000-8000-000000000281", want: inventory.ErrManualSyncSourceUnconfigured},
		{name: "wrong kind", sourceID: "00000000-0000-4000-8000-000000000282", want: inventory.ErrManualSyncSourceWrongKind},
		{name: "disabled", sourceID: "00000000-0000-4000-8000-000000000283", want: inventory.ErrManualSyncSourceDisabled},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := repo.RequestManualSync(ctx, inventory.ManualSyncRequest{ID: "00000000-0000-4000-8000-000000000285", SourceID: test.sourceID, RequestedAt: now})
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v, want errors.Is(_, %v)", err, test.want)
			}
		})
	}
}

func TestSourceUpdateLeavesManualSyncIntentAndAttemptEvidenceUntouched(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{28})
	keyID := cipher.KeyID()
	secretRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	sourceRepo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo)
	source := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000291", "00000000-0000-4000-8000-000000000292", 0)
	if _, err := sourceRepo.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	manualRepo := NewManualSyncRepository(&Pool{pool: pool})
	request := inventory.ManualSyncRequest{ID: "00000000-0000-4000-8000-000000000293", SourceID: source.Configuration.ID, RequestedAt: time.Now().UTC().Round(0)}
	if _, err := manualRepo.RequestManualSync(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err := manualRepo.RequestManualSync(ctx, request); err != nil {
		t.Fatal(err)
	}
	coalesced := request
	coalesced.ID = "00000000-0000-4000-8000-000000000294"
	coalesced.RequestedAt = coalesced.RequestedAt.Add(time.Second)
	if _, err := manualRepo.RequestManualSync(ctx, coalesced); err != nil {
		t.Fatal(err)
	}
	beforeIntent, beforeAttempts := manualSyncEvidence(t, ctx, pool, source.Configuration.ID)

	update := sourceConfigurationSave(t, cipher, source.Configuration.ID, "00000000-0000-4000-8000-000000000295", 1)
	update.Configuration.Name = "Updated Books"
	update.Configuration.SelectedLibraries = []string{"updated-library"}
	if result, err := sourceRepo.SaveSource(ctx, update); err != nil || result.Revision != 2 {
		t.Fatalf("source update=%+v err=%v", result, err)
	}
	afterIntent, afterAttempts := manualSyncEvidence(t, ctx, pool, source.Configuration.ID)
	if beforeIntent != afterIntent || len(beforeAttempts) != len(afterAttempts) {
		t.Fatalf("manual evidence changed intent %q/%q attempts %q/%q", beforeIntent, afterIntent, beforeAttempts, afterAttempts)
	}
	for i := range beforeAttempts {
		if beforeAttempts[i] != afterAttempts[i] {
			t.Fatalf("manual request attempt evidence changed at %d: %q != %q", i, beforeAttempts[i], afterAttempts[i])
		}
	}
}

func manualSyncEvidence(t *testing.T, ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, sourceID string) (string, []string) {
	t.Helper()
	var intent string
	var attempts []string
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT id::text || ':' || state || ':' || requested_at::text FROM inventory_manual_sync_intents WHERE source_id=$1),
		(SELECT array_agg(request_id::text || ':' || disposition || ':' || pending_intent_id::text || ':' || requested_at::text ORDER BY id) FROM inventory_manual_sync_request_attempts WHERE source_id=$1)`, sourceID).Scan(&intent, &attempts); err != nil {
		t.Fatal(err)
	}
	return intent, attempts
}

func TestManualSyncRepositoryConcurrentRequestsCoalesceAndReplayWithoutConfigurationMutation(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{27})
	keyID := cipher.KeyID()
	secretRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	sourceRepo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo)
	source := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000271", "00000000-0000-4000-8000-000000000272", 0)
	if _, err := sourceRepo.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	repo := NewManualSyncRepository(&Pool{pool: pool})
	now := time.Now().UTC().Round(0)
	distinct := []inventory.ManualSyncRequest{
		{ID: "00000000-0000-4000-8000-000000000273", SourceID: source.Configuration.ID, RequestedAt: now},
		{ID: "00000000-0000-4000-8000-000000000274", SourceID: source.Configuration.ID, RequestedAt: now.Add(time.Second)},
	}
	results := concurrentManualSyncRequests(t, ctx, repo, distinct)
	var recorded, coalesced int
	for _, result := range results {
		if result.PendingIntentID == "" {
			t.Fatalf("distinct concurrent result omitted pending intent: %+v", result)
		}
		switch result.Disposition {
		case inventory.ManualSyncRecorded:
			recorded++
		case inventory.ManualSyncCoalesced:
			coalesced++
		default:
			t.Fatalf("distinct concurrent disposition=%q", result.Disposition)
		}
	}
	if recorded != 1 || coalesced != 1 || results[0].PendingIntentID != results[1].PendingIntentID {
		t.Fatalf("distinct concurrent recorded/coalesced=%d/%d results=%+v", recorded, coalesced, results)
	}

	identical := inventory.ManualSyncRequest{ID: "00000000-0000-4000-8000-000000000275", SourceID: source.Configuration.ID, RequestedAt: now.Add(2 * time.Second)}
	results = concurrentManualSyncRequests(t, ctx, repo, []inventory.ManualSyncRequest{identical, identical})
	var initial, replayed int
	for _, result := range results {
		if result.PendingIntentID != results[0].PendingIntentID {
			t.Fatalf("identical concurrent requests returned different pending intent IDs: %+v", results)
		}
		switch result.Disposition {
		case inventory.ManualSyncCoalesced:
			initial++
		case inventory.ManualSyncReplayed:
			replayed++
		default:
			t.Fatalf("identical concurrent disposition=%q", result.Disposition)
		}
	}
	if initial != 1 || replayed != 1 {
		t.Fatalf("identical concurrent coalesced/replayed=%d/%d", initial, replayed)
	}

	var intents, recordedAttempts, coalescedAttempts, replayedAttempts, revision, audits, libraries, credentials int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM inventory_manual_sync_intents WHERE source_id=$1 AND state='pending'),
		(SELECT count(*) FROM inventory_manual_sync_request_attempts WHERE source_id=$1 AND disposition='recorded'),
		(SELECT count(*) FROM inventory_manual_sync_request_attempts WHERE source_id=$1 AND disposition='coalesced'),
		(SELECT count(*) FROM inventory_manual_sync_request_attempts WHERE source_id=$1 AND disposition='replayed'),
		(SELECT revision FROM inventory_source_configurations WHERE source_id=$1),
		(SELECT count(*) FROM inventory_source_configuration_audit WHERE source_id=$1),
		(SELECT count(*) FROM inventory_source_selected_libraries WHERE source_id=$1),
		(SELECT count(*) FROM encrypted_secrets WHERE namespace='inventory-source' AND owner_id=$1::uuid)`, source.Configuration.ID).Scan(&intents, &recordedAttempts, &coalescedAttempts, &replayedAttempts, &revision, &audits, &libraries, &credentials); err != nil {
		t.Fatal(err)
	}
	if intents != 1 || recordedAttempts != 1 || coalescedAttempts != 2 || replayedAttempts != 1 || revision != 1 || audits != 1 || libraries != 1 || credentials != 1 {
		t.Fatalf("intents/recorded/coalesced/replayed/revision/audits/libraries/credentials=%d/%d/%d/%d/%d/%d/%d/%d", intents, recordedAttempts, coalescedAttempts, replayedAttempts, revision, audits, libraries, credentials)
	}
}

func TestSourceSaveAndManualSyncSerializeOnSourceLock(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	cipher := secrets.New([32]byte{30})
	keyID := cipher.KeyID()
	secretsRepo := NewEncryptedSecretRepository(&Pool{pool: pool}, &keyID)
	if err := secretsRepo.EstablishActiveKey(ctx); err != nil {
		t.Fatal(err)
	}
	sourceRepo := NewSourceConfigurationRepository(&Pool{pool: pool}, secretsRepo)
	source := sourceConfigurationSave(t, cipher, "00000000-0000-4000-8000-000000000301", "00000000-0000-4000-8000-000000000302", 0)
	if _, err := sourceRepo.SaveSource(ctx, source); err != nil {
		t.Fatal(err)
	}

	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lockTx.Rollback(context.Background()) }()
	if _, err := lockTx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, source.Configuration.ID); err != nil {
		t.Fatal(err)
	}

	updated := sourceConfigurationSave(t, cipher, source.Configuration.ID, "00000000-0000-4000-8000-000000000303", 1)
	updated.Configuration.Name = "Updated while sync requested"
	manualRequest := inventory.ManualSyncRequest{ID: "00000000-0000-4000-8000-000000000304", SourceID: source.Configuration.ID, RequestedAt: time.Now().UTC()}
	completed := make(chan error, 2)
	go func() {
		_, err := sourceRepo.SaveSource(ctx, updated)
		completed <- err
	}()
	go func() {
		_, err := NewManualSyncRepository(&Pool{pool: pool}).RequestManualSync(ctx, manualRequest)
		completed <- err
	}()

	select {
	case err := <-completed:
		t.Fatalf("operation completed while source lock held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := lockTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-completed; err != nil {
			t.Fatalf("serialized operation failed: %v", err)
		}
	}
	var revision, intents, attempts int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT revision FROM inventory_source_configurations WHERE source_id=$1),
		(SELECT count(*) FROM inventory_manual_sync_intents WHERE source_id=$1),
		(SELECT count(*) FROM inventory_manual_sync_request_attempts WHERE request_id=$2)`, source.Configuration.ID, manualRequest.ID).Scan(&revision, &intents, &attempts); err != nil {
		t.Fatal(err)
	}
	if revision != 2 || intents != 1 || attempts != 1 {
		t.Fatalf("serialized state revision/intents/attempts=%d/%d/%d", revision, intents, attempts)
	}
}

func concurrentManualSyncRequests(t *testing.T, ctx context.Context, repo *ManualSyncRepository, requests []inventory.ManualSyncRequest) []inventory.ManualSyncRequestResult {
	t.Helper()
	start := make(chan struct{})
	results := make([]inventory.ManualSyncRequestResult, len(requests))
	errs := make([]error, len(requests))
	var group sync.WaitGroup
	for i, request := range requests {
		group.Add(1)
		go func(i int, request inventory.ManualSyncRequest) {
			defer group.Done()
			<-start
			results[i], errs[i] = repo.RequestManualSync(ctx, request)
		}(i, request)
	}
	close(start)
	group.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent manual sync request: %v", err)
		}
	}
	return results
}
