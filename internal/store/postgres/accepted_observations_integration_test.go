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

func TestAcceptedObservationCommitProjectionReplayAndIntegrity(t *testing.T) {
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
	sourceID := "00000000-0000-4000-8000-000000000501"
	save := sourceConfigurationSave(t, cipher, sourceID, "00000000-0000-4000-8000-000000000502", 0)
	save.Configuration.SelectedLibraries = []string{"books"}
	if _, err := NewSourceConfigurationRepository(&Pool{pool: pool}, secretRepo).SaveSource(ctx, save); err != nil {
		t.Fatal(err)
	}
	repo := NewInventoryRepository(&Pool{pool: pool})
	scope := inventory.ObservationScope{SourceID: sourceID, ConfigurationRevision: 1, SelectedLibraries: []string{"books"}, EnabledMediaTypes: []domain.MediaType{domain.MediaAudiobook}}
	command := inventory.AcceptedObservationCommit{ID: "00000000-0000-4000-8000-000000000503", Observation: inventory.CompleteObservation{Scope: scope, Coverage: []inventory.ObservationCoverage{{RemoteLibraryID: "books", MediaType: domain.MediaAudiobook, Status: inventory.CoverageVerifiedComplete}}, Items: []inventory.ObservedItem{{RemoteLibraryID: "books", ItemID: "item-1", MediaType: domain.MediaAudiobook, Title: "One", Revision: "1"}}}}
	result, err := repo.CommitAcceptedObservation(ctx, command)
	if err != nil || !result.Applied || result.Added != 1 {
		t.Fatalf("first result=%+v err=%v", result, err)
	}
	replay, err := repo.CommitAcceptedObservation(ctx, command)
	if err != nil || replay.Applied || replay.Added != 1 {
		t.Fatalf("replay result=%+v err=%v", replay, err)
	}
	changed := command
	changed.Observation.Items[0].Title = "Changed"
	if _, err := repo.CommitAcceptedObservation(ctx, changed); !errors.Is(err, inventory.ErrObservationCommitConflict) {
		t.Fatalf("changed replay error=%v", err)
	}
	second := command
	second.ID = "00000000-0000-4000-8000-000000000504"
	second.Observation.Items[0].Title, second.Observation.Items[0].Revision = "Changed", "2"
	result, err = repo.CommitAcceptedObservation(ctx, second)
	if err != nil || !result.Applied || result.Updated != 1 {
		t.Fatalf("update result=%+v err=%v", result, err)
	}
	third := second
	third.ID = "00000000-0000-4000-8000-000000000505"
	result, err = repo.CommitAcceptedObservation(ctx, third)
	if err != nil || !result.Applied || result.Added != 0 || result.Updated != 0 || result.Removed != 0 {
		t.Fatalf("unchanged result=%+v err=%v", result, err)
	}
	var firstOrder, secondOrder, thirdOrder int64
	var firstAcceptedAt, secondAcceptedAt, thirdAcceptedAt time.Time
	var secondPredecessor, thirdPredecessor string
	if err := pool.QueryRow(ctx, `SELECT acceptance_order, accepted_at FROM inventory_accepted_observations WHERE id=$1`, command.ID).Scan(&firstOrder, &firstAcceptedAt); err != nil {
		t.Fatalf("read first server metadata: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT acceptance_order, accepted_at, predecessor_observation_id::text FROM inventory_accepted_observations WHERE id=$1`, second.ID).Scan(&secondOrder, &secondAcceptedAt, &secondPredecessor); err != nil {
		t.Fatalf("read second server metadata: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT acceptance_order, accepted_at, predecessor_observation_id::text FROM inventory_accepted_observations WHERE id=$1`, third.ID).Scan(&thirdOrder, &thirdAcceptedAt, &thirdPredecessor); err != nil {
		t.Fatalf("read third server metadata: %v", err)
	}
	if firstOrder <= 0 || firstOrder >= secondOrder || secondOrder >= thirdOrder || secondPredecessor != command.ID || thirdPredecessor != second.ID || firstAcceptedAt.IsZero() || secondAcceptedAt.IsZero() || thirdAcceptedAt.IsZero() {
		t.Fatalf("server metadata orders=%d/%d/%d predecessors=%q/%q accepted=%v/%v/%v", firstOrder, secondOrder, thirdOrder, secondPredecessor, thirdPredecessor, firstAcceptedAt, secondAcceptedAt, thirdAcceptedAt)
	}
	var latestProvenance string
	if err := pool.QueryRow(ctx, `SELECT last_observation_id::text FROM inventory_desired_source_revision_items WHERE source_id=$1 AND configuration_revision=1 AND source_item_id='item-1'`, sourceID).Scan(&latestProvenance); err != nil || latestProvenance != third.ID {
		t.Fatalf("unchanged item provenance=%q err=%v, want %q", latestProvenance, err, third.ID)
	}
	concurrent := [2]inventory.AcceptedObservationCommit{third, third}
	concurrent[0].ID = "00000000-0000-4000-8000-000000000506"
	concurrent[1].ID = "00000000-0000-4000-8000-000000000507"
	start := make(chan struct{})
	errs := make(chan error, len(concurrent))
	var workers sync.WaitGroup
	for _, candidate := range concurrent {
		workers.Add(1)
		go func(candidate inventory.AcceptedObservationCommit) {
			defer workers.Done()
			<-start
			_, err := repo.CommitAcceptedObservation(ctx, candidate)
			errs <- err
		}(candidate)
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent unchanged commit: %v", err)
		}
	}
	var desired, evidence, events int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM inventory_desired_source_revision_items),(SELECT count(*) FROM inventory_accepted_observations),(SELECT count(*) FROM inventory_accepted_observation_events)`).Scan(&desired, &evidence, &events); err != nil || desired != 1 || evidence != 5 || events != 2 {
		t.Fatalf("counts desired/evidence/events=%d/%d/%d err=%v", desired, evidence, events, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_accepted_observation_items SET title='mutated' WHERE observation_id=$1`, command.ID); err == nil {
		t.Fatal("direct evidence update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM inventory_accepted_observation_coverage WHERE observation_id=$1`, command.ID); err == nil {
		t.Fatal("direct evidence delete unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_desired_source_revision_items(source_id,configuration_revision,remote_library_id,source_item_id,media_type,title,external_url,item_revision,last_observation_id) VALUES($1,1,'books','forged','audiobook','Forged','', '1',$2)`, sourceID, command.ID); err == nil {
		t.Fatal("direct malformed desired-state provenance unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM inventory_desired_source_revision_items WHERE source_id=$1 AND configuration_revision=1`, sourceID); err == nil {
		t.Fatal("direct desired-state deletion unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_accepted_observation_events(observation_id,ordinal,change_kind,remote_library_id,source_item_id,media_type) VALUES($1,99,'added','books','forged','audiobook')`, command.ID); err == nil {
		t.Fatal("forged accepted-observation event unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_accepted_observation_events(observation_id,ordinal,change_kind,remote_library_id,source_item_id,media_type) VALUES($1,100,'added','outside','forged','audiobook')`, command.ID); err == nil {
		t.Fatal("out-of-scope accepted-observation event unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_accepted_observations(id,source_id,configuration_revision,command_fingerprint,accepted_at) VALUES('00000000-0000-4000-8000-000000000599',$1,1,decode(repeat('00',32),'hex'),clock_timestamp())`, sourceID); err == nil {
		t.Fatal("explicit accepted_at unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_accepted_observations(id,source_id,configuration_revision,command_fingerprint,acceptance_order) VALUES('00000000-0000-4000-8000-000000000599',$1,1,decode(repeat('00',32),'hex'),1)`, sourceID); err == nil {
		t.Fatal("historical acceptance order injection unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_accepted_observations(id,source_id,configuration_revision,command_fingerprint) VALUES('00000000-0000-4000-8000-000000000599',$1,1,decode(repeat('00',32),'hex'))`, sourceID); err == nil {
		t.Fatal("parent-only accepted observation unexpectedly succeeded")
	}
}
