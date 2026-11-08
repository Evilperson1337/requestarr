package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/inventory/jellyfin"
	"github.com/evilperson1337/requestarr/internal/jobs"
)

type fakeSyncStore struct {
	persisted  []inventory.Snapshot
	failures   []string
	persistErr error
}

func (f *fakeSyncStore) PersistSnapshot(_ context.Context, _ inventory.Descriptor, _ string, snap inventory.Snapshot) error {
	f.persisted = append(f.persisted, snap)
	return f.persistErr
}

func (f *fakeSyncStore) RecordSyncFailure(_ context.Context, _ inventory.Descriptor, _ time.Time, safe string) error {
	f.failures = append(f.failures, safe)
	return nil
}

type fakeSource struct {
	snap inventory.Snapshot
	err  error
}

func (fakeSource) Descriptor() inventory.Descriptor {
	return inventory.Descriptor{ID: jellyfin.SourceID, Kind: "jellyfin", Name: "Jellyfin"}
}
func (f fakeSource) Snapshot(context.Context) (inventory.Snapshot, error) { return f.snap, f.err }

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestSourceSyncJobPersistsTheSourcesCompleteSnapshot(t *testing.T) {
	store := &fakeSyncStore{}
	snap := inventory.Snapshot{SourceID: jellyfin.SourceID, Complete: true, Items: []inventory.Item{{SourceID: jellyfin.SourceID, SourceItemID: "1"}}}
	if err := sourceSyncJob(discardLogger, store, fakeSource{snap: snap}, "http://jellyfin.example")(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.persisted) != 1 || !store.persisted[0].Complete || len(store.persisted[0].Items) != 1 {
		t.Fatalf("persisted=%+v", store.persisted)
	}
}

func TestSourceSyncJobReportsOnlySafeErrorsAndChangesNothingOnFailure(t *testing.T) {
	store := &fakeSyncStore{}
	err := sourceSyncJob(discardLogger, store, fakeSource{err: errors.New("GET http://jellyfin.example/Items?api_key=secret: 500")}, "http://jellyfin.example")(context.Background())
	if err == nil || err.Error() != "inventory request failed" {
		t.Fatalf("error=%v, want the generic safe message", err)
	}
	if len(store.persisted) != 0 || len(store.failures) != 1 || store.failures[0] != "inventory request failed" {
		t.Fatalf("persisted=%d failures=%v", len(store.persisted), store.failures)
	}
	store = &fakeSyncStore{persistErr: errors.New("pq: connection string postgres://u:p@db")}
	err = sourceSyncJob(discardLogger, store, fakeSource{snap: inventory.Snapshot{SourceID: jellyfin.SourceID, Complete: true}}, "http://jellyfin.example")(context.Background())
	if err == nil || err.Error() != "snapshot persistence failed" {
		t.Fatalf("error=%v, want the generic safe message", err)
	}
}

func TestSourceSyncJobRefusesASnapshotThatIsNotComplete(t *testing.T) {
	store := &fakeSyncStore{}
	partial := inventory.Snapshot{SourceID: jellyfin.SourceID, Complete: false, Items: []inventory.Item{{SourceItemID: "1"}}}
	if err := sourceSyncJob(discardLogger, store, fakeSource{snap: partial}, "http://jellyfin.example")(context.Background()); err == nil {
		t.Fatal("an incomplete snapshot was treated as a successful sync")
	}
	if len(store.persisted) != 0 {
		t.Fatal("an incomplete snapshot reached the store")
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
		"jellyfin": {id: jellyfin.SourceID, job: func(context.Context) error { ran <- struct{}{}; <-release; return nil }},
	})
	if _, err := syncer.SyncNow(context.Background(), "plex"); !errors.Is(err, jobs.ErrUnknownSource) {
		t.Fatalf("unknown source err=%v", err)
	}
	run, err := syncer.SyncNow(context.Background(), "jellyfin")
	if err != nil || run.Trigger != jobs.TriggerManual || run.SourceID != jellyfin.SourceID {
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
	syncer := newSourceSyncer(context.Background(), runner, &workers, map[string]syncSource{"jellyfin": {id: jellyfin.SourceID, job: job}})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runner.Run(context.Background(), jellyfin.SourceID, jobs.TriggerSchedule, job)
	}()
	<-started
	if _, err := syncer.SyncNow(context.Background(), "jellyfin"); !errors.Is(err, jobs.ErrAlreadyRunning) {
		t.Fatalf("manual sync during a scheduled run err=%v", err)
	}
	close(release)
	<-done
	workers.Wait()
}

type fakeAliasStore struct {
	pending []domain.CatalogRef
	limit   int
	saved   map[string][]string
	failed  []string
}

func (f *fakeAliasStore) OpenCatalogItemsNeedingAliases(_ context.Context, provider string, limit int) ([]domain.CatalogRef, error) {
	f.limit = limit
	if provider != "musicbrainz" {
		return nil, errors.New("unexpected provider " + provider)
	}
	return f.pending, nil
}

func (f *fakeAliasStore) SaveCatalogAliases(_ context.Context, id, kind string, values []string) error {
	if kind != "mbid" {
		return errors.New("unexpected alias kind " + kind)
	}
	if f.saved == nil {
		f.saved = map[string][]string{}
	}
	f.saved[id] = values
	return nil
}

func (f *fakeAliasStore) RecordAliasLookupFailure(_ context.Context, id string) error {
	f.failed = append(f.failed, id)
	return nil
}

type fakeAliasLookup struct {
	failFor map[string]bool
	calls   []string
}

func (f *fakeAliasLookup) Aliases(_ context.Context, ref domain.CatalogRef) ([]string, error) {
	f.calls = append(f.calls, ref.ID)
	if f.failFor[ref.ID] {
		return nil, errors.New("MusicBrainz release lookup failed")
	}
	return []string{ref.ProviderID, "release-of-" + ref.ID}, nil
}

func TestAliasRefreshSavesLookupsAndSurvivesIndividualFailures(t *testing.T) {
	store := &fakeAliasStore{pending: []domain.CatalogRef{{ID: "a", ProviderID: "pa"}, {ID: "b", ProviderID: "pb"}, {ID: "c", ProviderID: "pc"}}}
	lookup := &fakeAliasLookup{failFor: map[string]bool{"b": true}}
	refreshMusicAliases(context.Background(), discardLogger, store, lookup)
	if store.limit != maxAliasLookupsPerSync {
		t.Fatalf("limit=%d want %d", store.limit, maxAliasLookupsPerSync)
	}
	if len(lookup.calls) != 3 {
		t.Fatalf("a failed lookup must not stop the others: calls=%v", lookup.calls)
	}
	if _, ok := store.saved["b"]; ok {
		t.Fatal("a failed lookup saved aliases")
	}
	if len(store.failed) != 1 || store.failed[0] != "b" {
		t.Fatalf("failed lookups recorded for backoff: %v (want only b)", store.failed)
	}
	if len(store.saved["a"]) != 2 || len(store.saved["c"]) != 2 {
		t.Fatalf("saved=%v", store.saved)
	}
}

func TestAliasRefreshStopsWhenShuttingDown(t *testing.T) {
	store := &fakeAliasStore{pending: []domain.CatalogRef{{ID: "a"}, {ID: "b"}}}
	lookup := &fakeAliasLookup{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	refreshMusicAliases(ctx, discardLogger, store, lookup)
	if len(lookup.calls) != 0 {
		t.Fatalf("lookups after cancellation: %v", lookup.calls)
	}
}

func TestSyncWithAliasRefreshRefreshesFirstAndStillSyncsWhenRefreshFails(t *testing.T) {
	var order []string
	job := func(context.Context) error { order = append(order, "sync"); return nil }
	refresh := func(context.Context) { order = append(order, "refresh") }
	if err := withAliasRefresh(refresh, job)(context.Background()); err != nil || len(order) != 2 || order[0] != "refresh" || order[1] != "sync" {
		t.Fatalf("order=%v err=%v", order, err)
	}
}

// stepSource reports where it is, then fails there.
type stepSource struct{ fakeSource }

func (s stepSource) Snapshot(ctx context.Context) (inventory.Snapshot, error) {
	inventory.SetStep(ctx, "listing platforms")
	inventory.SetStep(ctx, "listing games at offset %d", 400)
	return inventory.Snapshot{}, errors.New("RomM inventory returned HTTP 500")
}

// The log says where in the enumeration a sync failed, while the stored error
// stays the fixed string.
func TestAFailedSyncLogsTheStepItWasOn(t *testing.T) {
	var logs bytes.Buffer
	store := &fakeSyncStore{}
	err := sourceSyncJob(slog.New(slog.NewTextHandler(&logs, nil)), store, stepSource{}, "http://jellyfin.example")(context.Background())
	if err == nil || err.Error() != "inventory request failed" {
		t.Fatalf("error = %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, `step="listing games at offset 400"`) || !strings.Contains(out, "HTTP 500") {
		t.Fatalf("log = %q", out)
	}
	if len(store.failures) != 1 || strings.Contains(store.failures[0], "offset") {
		t.Fatalf("stored failure = %v", store.failures)
	}
}
