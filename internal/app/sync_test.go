package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/inventory"
)

type fakeSyncStore struct {
	persisted  []inventory.Snapshot
	failures   []string
	persistErr error
}

func (f *fakeSyncStore) PersistJellyfinSnapshot(_ context.Context, _ string, snap inventory.Snapshot) error {
	f.persisted = append(f.persisted, snap)
	return f.persistErr
}

func (f *fakeSyncStore) RecordJellyfinFailure(_ context.Context, _ time.Time, safe string) error {
	f.failures = append(f.failures, safe)
	return nil
}

type fakeSnapshotter struct {
	items []inventory.Item
	err   error
}

func (f fakeSnapshotter) Snapshot(context.Context) ([]inventory.Item, error) { return f.items, f.err }

func TestJellyfinSyncJobPersistsCompleteSnapshot(t *testing.T) {
	store := &fakeSyncStore{}
	items := []inventory.Item{{SourceID: jellyfinSourceID, SourceItemID: "1"}}
	if err := jellyfinSyncJob(slog.New(slog.NewTextHandler(io.Discard, nil)), store, fakeSnapshotter{items: items}, "http://jellyfin.example")(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.persisted) != 1 || !store.persisted[0].Complete || store.persisted[0].SourceID != jellyfinSourceID || len(store.persisted[0].Items) != 1 {
		t.Fatalf("persisted=%+v", store.persisted)
	}
}

func TestJellyfinSyncJobReportsOnlySafeErrorsAndChangesNothingOnFailure(t *testing.T) {
	store := &fakeSyncStore{}
	err := jellyfinSyncJob(slog.New(slog.NewTextHandler(io.Discard, nil)), store, fakeSnapshotter{err: errors.New("GET http://jellyfin.example/Items?api_key=secret: 500")}, "http://jellyfin.example")(context.Background())
	if err == nil || err.Error() != "inventory request failed" {
		t.Fatalf("error=%v, want the generic safe message", err)
	}
	if len(store.persisted) != 0 || len(store.failures) != 1 || store.failures[0] != "inventory request failed" {
		t.Fatalf("persisted=%d failures=%v", len(store.persisted), store.failures)
	}
	store = &fakeSyncStore{persistErr: errors.New("pq: connection string postgres://u:p@db")}
	err = jellyfinSyncJob(slog.New(slog.NewTextHandler(io.Discard, nil)), store, fakeSnapshotter{}, "http://jellyfin.example")(context.Background())
	if err == nil || err.Error() != "snapshot persistence failed" {
		t.Fatalf("error=%v, want the generic safe message", err)
	}
}
