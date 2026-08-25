//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
	"git.example.com/evilperson1337/Requestarr/internal/store/postgres/migrations"
)

func TestInventoryRepositoryCyclesAndStatusTransitions(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	sourceID := "00000000-0000-4000-8000-000000000101"
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,authoritative_media_types,created_at,updated_at) VALUES($1,'plex','Plex','https://plex.example','["movie"]',$2,$2)`, sourceID, now); err != nil {
		t.Fatal(err)
	}
	repo := NewInventoryRepository(&Pool{pool: pool})
	cycle := inventory.AuthoritativeCycle{ID: "00000000-0000-4000-8000-000000000102", SourceID: sourceID, StartedAt: now.Add(-time.Minute), CompletedAt: now, Complete: true, Events: []inventory.InventoryEvent{{Kind: inventory.ChangeAdded, SourceItemID: "movie-1", MediaType: domain.MediaMovie}}}
	commit, err := repo.CommitAuthoritativeCycle(ctx, cycle)
	if err != nil || !commit.Committed {
		t.Fatalf("first commit=%+v err=%v", commit, err)
	}
	commit, err = repo.CommitAuthoritativeCycle(ctx, cycle)
	if err != nil || commit.Committed {
		t.Fatalf("replay commit=%+v err=%v", commit, err)
	}
	changed := cycle
	changed.Events = []inventory.InventoryEvent{{Kind: inventory.ChangeUpdated, SourceItemID: "movie-1", MediaType: domain.MediaMovie}}
	if _, err := repo.CommitAuthoritativeCycle(ctx, changed); !errors.Is(err, ErrInventoryPersistenceInvalid) {
		t.Fatalf("changed cycle replay error=%v", err)
	}
	var cycles, events int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM inventory_authoritative_cycles),(SELECT count(*) FROM inventory_authoritative_cycle_events)`).Scan(&cycles, &events); err != nil || cycles != 1 || events != 1 {
		t.Fatalf("cycle/event counts=%d/%d err=%v", cycles, events, err)
	}
	running := inventory.SourceStatusTransition{SourceID: sourceID, CycleID: "00000000-0000-4000-8000-000000000103", From: inventory.SourceSyncIdle, To: inventory.SourceSyncRunning, OccurredAt: now}
	result, err := repo.TransitionSourceStatus(ctx, running)
	if err != nil || !result.Applied || result.Current.State != inventory.SourceSyncRunning {
		t.Fatalf("running=%+v err=%v", result, err)
	}
	replayed, err := repo.TransitionSourceStatus(ctx, running)
	if err != nil || replayed.Applied || replayed.Current.State != inventory.SourceSyncRunning {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	stale := running
	stale.CycleID = "00000000-0000-4000-8000-000000000104"
	if _, err = repo.TransitionSourceStatus(ctx, stale); !errors.Is(err, ErrInventoryStatusConflict) {
		t.Fatalf("stale transition error=%v", err)
	}
	failed := inventory.SourceStatusTransition{SourceID: sourceID, CycleID: "00000000-0000-4000-8000-000000000105", From: inventory.SourceSyncRunning, To: inventory.SourceSyncFailed, OccurredAt: now.Add(time.Minute), FailureDetail: string(inventory.SourceFailureRequestTimedOut)}
	result, err = repo.TransitionSourceStatus(ctx, failed)
	if err != nil || !result.Applied || result.Current.State != inventory.SourceSyncFailed || result.Current.FailureDetail != string(inventory.SourceFailureRequestTimedOut) {
		t.Fatalf("failed=%+v err=%v", result, err)
	}
	unsafe := failed
	unsafe.CycleID = "00000000-0000-4000-8000-000000000106"
	unsafe.FailureDetail = "GET https://source.example?token=secret: timeout"
	if _, err := repo.TransitionSourceStatus(ctx, unsafe); !errors.Is(err, ErrInventoryPersistenceInvalid) {
		t.Fatalf("unsafe failure detail error=%v", err)
	}
}
