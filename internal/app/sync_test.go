package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/jobs"
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

// memRuns is a minimal jobs.Store: one running run per source.
type memRuns struct {
	mu      sync.Mutex
	running map[string]bool
	started []jobs.Run
}

func (m *memRuns) StartRun(_ context.Context, sourceID, trigger string, _ time.Duration) (jobs.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running == nil {
		m.running = map[string]bool{}
	}
	if m.running[sourceID] {
		return jobs.Run{}, jobs.ErrAlreadyRunning
	}
	m.running[sourceID] = true
	run := jobs.Run{ID: "run", SourceID: sourceID, Trigger: trigger, Status: jobs.StatusRunning}
	m.started = append(m.started, run)
	return run, nil
}
func (m *memRuns) Heartbeat(context.Context, string, time.Duration) error { return nil }
func (m *memRuns) FinishRun(_ context.Context, _ string, _ jobs.Status, _ string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = map[string]bool{}
	return time.Now(), nil
}
func (m *memRuns) RecentRuns(context.Context, string, int) ([]jobs.Run, error) { return nil, nil }

func TestSyncNowStartsManualRunOnceAndRejectsUnknownSources(t *testing.T) {
	var workers sync.WaitGroup
	store := &memRuns{}
	release := make(chan struct{})
	ran := make(chan struct{}, 1)
	syncer := newSourceSyncer(context.Background(), jobs.NewRunner(store), &workers, map[string]syncSource{
		"jellyfin": {id: jellyfinSourceID, job: func(context.Context) error { ran <- struct{}{}; <-release; return nil }},
	})
	if _, err := syncer.SyncNow(context.Background(), "plex"); !errors.Is(err, jobs.ErrUnknownSource) {
		t.Fatalf("unknown source err=%v", err)
	}
	run, err := syncer.SyncNow(context.Background(), "jellyfin")
	if err != nil || run.Trigger != jobs.TriggerManual || run.SourceID != jellyfinSourceID {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	<-ran
	if _, err := syncer.SyncNow(context.Background(), "jellyfin"); !errors.Is(err, jobs.ErrAlreadyRunning) {
		t.Fatalf("second sync err=%v", err)
	}
	close(release)
	workers.Wait() // shutdown waits for background syncs
}

func TestManualSyncIsBlockedByARunningScheduledRun(t *testing.T) {
	var workers sync.WaitGroup
	store := &memRuns{}
	runner := jobs.NewRunner(store)
	release, started := make(chan struct{}), make(chan struct{})
	job := func(context.Context) error { close(started); <-release; return nil }
	syncer := newSourceSyncer(context.Background(), runner, &workers, map[string]syncSource{"jellyfin": {id: jellyfinSourceID, job: job}})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runner.Run(context.Background(), jellyfinSourceID, jobs.TriggerSchedule, job)
	}()
	<-started
	if _, err := syncer.SyncNow(context.Background(), "jellyfin"); !errors.Is(err, jobs.ErrAlreadyRunning) {
		t.Fatalf("manual sync during a scheduled run err=%v", err)
	}
	close(release)
	<-done
	workers.Wait()
}
