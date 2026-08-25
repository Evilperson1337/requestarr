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

func TestAudiobookSnapshotRepositoryAtomicLifecycle(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,authoritative_media_types,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000001','audiobookshelf','Books','https://books.example','["audiobook"]',$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	repo := NewAudiobookSnapshotRepository(&Pool{pool: pool})
	first := testAudiobookSnapshot(now, "book-1")
	committed, err := repo.CommitAudiobookSnapshot(ctx, first)
	if err != nil || !committed.Committed {
		t.Fatalf("first commit=%+v err=%v", committed, err)
	}
	idempotent, err := repo.CommitAudiobookSnapshot(ctx, first)
	if err != nil || idempotent.Committed || idempotent.SnapshotID != committed.SnapshotID {
		t.Fatalf("idempotent=%+v err=%v", idempotent, err)
	}
	second := testAudiobookSnapshot(now.Add(time.Minute), "book-2")
	if _, err = repo.CommitAudiobookSnapshot(ctx, second); err != nil {
		t.Fatal(err)
	}
	var tombstoned, live int
	if err = pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE tombstoned_at IS NOT NULL), count(*) FILTER (WHERE tombstoned_at IS NULL) FROM inventory_items`).Scan(&tombstoned, &live); err != nil {
		t.Fatal(err)
	}
	if tombstoned != 1 || live != 1 {
		t.Fatalf("tombstoned=%d live=%d", tombstoned, live)
	}
	stale := testAudiobookSnapshot(now.Add(30*time.Second), "book-3")
	staleCommit, err := repo.CommitAudiobookSnapshot(ctx, stale)
	if err != nil || staleCommit.Committed {
		t.Fatalf("stale commit=%+v err=%v", staleCommit, err)
	}
	var title, itemID string
	if err = pool.QueryRow(ctx, `SELECT source_item_id, title FROM inventory_items WHERE tombstoned_at IS NULL`).Scan(&itemID, &title); err != nil || itemID != "book-2" || title != "Example" {
		t.Fatalf("live item=%q/%q err=%v", itemID, title, err)
	}
	incomplete := testAudiobookSnapshot(now.Add(2*time.Minute), "book-3")
	incomplete.Complete = false
	if _, err = repo.CommitAudiobookSnapshot(ctx, incomplete); !errors.Is(err, ErrAudiobookSnapshotInvalid) {
		t.Fatalf("incomplete error=%v", err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE tombstoned_at IS NOT NULL) FROM inventory_items`).Scan(&tombstoned); err != nil || tombstoned != 1 {
		t.Fatalf("tombstones after incomplete=%d err=%v", tombstoned, err)
	}
}

func testAudiobookSnapshot(at time.Time, itemID string) inventory.AudiobookSnapshot {
	return inventory.AudiobookSnapshot{SourceID: "00000000-0000-4000-8000-000000000001", CompletedAt: at, Complete: true, SelectedLibrary: []inventory.RemoteLibrary{{ID: "fiction", Name: "Fiction"}}, Items: []inventory.AudiobookItem{{RemoteLibraryID: "fiction", SourceItemID: itemID, Title: "Example", Details: domain.AudiobookDetails{ASIN: "B012345678", AudibleRegion: "US", Authors: []string{"Author"}}}}}
}
