package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/jobs"
	"github.com/evilperson1337/requestarr/internal/opstatus"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// mutableRepo is a boot store whose saved connection and stored secret change during a test.
type mutableRepo struct {
	mu    sync.Mutex
	conns map[string]sources.Connection
	rows  []secrets.Stored
}

func (r *mutableRepo) ListLibraryConnections(context.Context) (map[string]sources.Connection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]sources.Connection{}
	for k, v := range r.conns {
		out[k] = v
	}
	return out, nil
}
func (r *mutableRepo) LoadIGDBClientID(context.Context) (string, error) { return "", nil }
func (r *mutableRepo) ListStoredSecrets(context.Context) ([]secrets.Stored, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]secrets.Stored(nil), r.rows...), nil
}
func (r *mutableRepo) PutStoredSecret(context.Context, string, secrets.Name, secrets.Sealed) error {
	return nil
}
func (r *mutableRepo) DeleteStoredSecret(context.Context, string, secrets.Name) error { return nil }

func sealedPlex(t *testing.T, value string) []secrets.Stored {
	t.Helper()
	key, _ := secrets.ParseMasterKey(hexKeyA)
	sealer, _ := secrets.NewAESGCM(key)
	sealed, err := sealer.Seal(secrets.PlexToken, []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	return []secrets.Stored{{Name: secrets.PlexToken, Sealed: sealed, UpdatedAt: time.Now()}}
}

type liveSource struct{ id string }

func (s liveSource) Descriptor() inventory.Descriptor {
	return inventory.Descriptor{ID: s.id, Kind: "plex", Name: "Plex"}
}
func (liveSource) Snapshot(context.Context) (inventory.Snapshot, error) {
	return inventory.Snapshot{}, errors.New("not used")
}

type managerRig struct {
	m       *sourceManager
	repo    *mutableRepo
	built   int
	workers sync.WaitGroup
	cancel  context.CancelFunc
}

func newRig(t *testing.T, repo *mutableRepo) *managerRig {
	t.Helper()
	rig := &managerRig{repo: repo}
	ctx, cancel := context.WithCancel(context.Background())
	rig.cancel = cancel
	t.Cleanup(func() { cancel(); rig.workers.Wait() })
	store := &memRuns{}
	runner := jobs.NewRunner(store)
	runner.Logger = discardLogger
	syncer := newSourceSyncer(ctx, runner, &rig.workers, map[string]syncSource{})
	rig.m = &sourceManager{
		ctx: ctx, workers: &rig.workers, runner: runner, logger: discardLogger, envCfg: config.Config{SecretsKey: hexKeyA}, store: repo,
		adapters: Adapters{SyncStore: &fakeSyncStore{}, Logger: discardLogger, NewSource: func(config.Config, sources.Kind) (inventory.Source, sourceDecorator, error) {
			rig.built++
			return liveSource{id: "plex-id"}, nil, nil
		}},
		syncer: syncer, ops: opstatus.New(nil, nil, nil), secrets: nil, running: map[string]*running{},
	}
	return rig
}

func TestApplyStartsRestartsAndStopsASourceFromItsSavedConnection(t *testing.T) {
	repo := &mutableRepo{conns: map[string]sources.Connection{"plex": {Slug: "plex", URL: "https://plex.example.test"}}, rows: sealedPlex(t, "token")}
	rig := newRig(t, repo)
	if err := rig.m.Apply(context.Background(), "plex"); err != nil {
		t.Fatal(err)
	}
	if !rig.m.Active("plex") || rig.built != 1 {
		t.Fatalf("active=%v built=%d", rig.m.Active("plex"), rig.built)
	}
	if _, ok := rig.m.syncer.snapshot()["plex"]; !ok {
		t.Fatal("the syncer cannot start a manual sync of the new Source")
	}
	// A changed address rebuilds the client and keeps one schedule.
	repo.mu.Lock()
	repo.conns["plex"] = sources.Connection{Slug: "plex", URL: "https://other.example.test"}
	repo.mu.Unlock()
	if err := rig.m.Apply(context.Background(), "plex"); err != nil || rig.built != 2 || len(rig.m.running) != 1 {
		t.Fatalf("restart: err=%v built=%d running=%d", err, rig.built, len(rig.m.running))
	}
	// Removing the address stops the sync and forgets the Source.
	repo.mu.Lock()
	delete(repo.conns, "plex")
	repo.mu.Unlock()
	if err := rig.m.Apply(context.Background(), "plex"); err != nil || rig.m.Active("plex") {
		t.Fatalf("stop: err=%v active=%v", err, rig.m.Active("plex"))
	}
	if _, ok := rig.m.syncer.snapshot()["plex"]; ok {
		t.Fatal("a removed Source can still be synced")
	}
}

func TestApplyLeavesAnAddressWithoutItsCredentialOffAndSaysWhatIsMissing(t *testing.T) {
	repo := &mutableRepo{conns: map[string]sources.Connection{"plex": {Slug: "plex", URL: "https://plex.example.test"}}}
	rig := newRig(t, repo)
	var incomplete ErrIncomplete
	err := rig.m.Apply(context.Background(), "plex")
	if !errors.As(err, &incomplete) || len(incomplete.Missing) != 1 || incomplete.Missing[0] != "token" {
		t.Fatalf("err = %v (%+v)", err, incomplete)
	}
	if rig.m.Active("plex") || rig.built != 0 {
		t.Fatal("an incomplete connection must not start a sync")
	}
}

func TestApplyNeverOverridesAnEnvironmentAddress(t *testing.T) {
	repo := &mutableRepo{conns: map[string]sources.Connection{"plex": {Slug: "plex", URL: "https://saved.example.test"}}, rows: sealedPlex(t, "token")}
	rig := newRig(t, repo)
	rig.m.envCfg.PlexURL = "https://env.example.test"
	var seen string
	rig.m.adapters.NewSource = func(cfg config.Config, _ sources.Kind) (inventory.Source, sourceDecorator, error) {
		seen = cfg.PlexURL
		return liveSource{id: "plex-id"}, nil, nil
	}
	if err := rig.m.Apply(context.Background(), "plex"); err != nil || seen != "https://env.example.test" {
		t.Fatalf("err=%v url=%q", err, seen)
	}
}
