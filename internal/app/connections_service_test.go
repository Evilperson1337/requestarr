package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/httpapi"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// savingRepo stores the connection and the sealed credentials together, as the
// PostgreSQL store does, and can fail the whole write.
type savingRepo struct {
	*mutableRepo
	fail  error
	saves int
}

func (r *savingRepo) SaveLibraryConnection(_ context.Context, _ string, c *sources.Connection, sealed map[secrets.Name]secrets.Sealed) error {
	r.saves++
	if r.fail != nil {
		return r.fail
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if c != nil {
		r.conns[c.Slug] = *c
	}
	for name, s := range sealed {
		r.rows = append(r.rows, secrets.Stored{Name: name, Sealed: s, UpdatedAt: time.Now()})
	}
	return nil
}
func (r *savingRepo) DeleteLibraryConnection(context.Context, string) error { return nil }

func newConnectionsRig(t *testing.T, repo *savingRepo, key string) (*connectionsService, *managerRig) {
	t.Helper()
	rig := newRig(t, repo.mutableRepo)
	var sealer secrets.Sealer
	if key != "" {
		parsed, err := secrets.ParseMasterKey(key)
		if err != nil {
			t.Fatal(err)
		}
		sealer, _ = secrets.NewAESGCM(parsed)
	}
	svc := secrets.NewService(repo, sealer, nil)
	return &connectionsService{store: repo, boot: repo, envCfg: rig.m.envCfg, manager: rig.m, secrets: svc, logger: discardLogger}, rig
}

const plexTokenProbe = "plex-token-PROBE-77"

func TestAddressAndCredentialAreSavedAndAppliedOnce(t *testing.T) {
	repo := &savingRepo{mutableRepo: &mutableRepo{conns: map[string]sources.Connection{}}}
	svc, rig := newConnectionsRig(t, repo, hexKeyA)
	got, err := svc.SaveConnection(context.Background(), "admin", sources.Connection{Slug: "plex", URL: "https://plex.example.test"}, map[secrets.Name]string{secrets.PlexToken: plexTokenProbe})
	if err != nil {
		t.Fatal(err)
	}
	if repo.saves != 1 || rig.built != 1 || !got.Active || len(got.Missing) != 0 {
		t.Fatalf("saves=%d built=%d view=%+v", repo.saves, rig.built, got)
	}
	if len(got.Credentials) != 1 || !got.Credentials[0].Configured {
		t.Fatalf("credential not reported as configured: %+v", got.Credentials)
	}
	for _, row := range repo.rows {
		if string(row.Ciphertext) == plexTokenProbe {
			t.Fatal("the credential reached the store unsealed")
		}
	}
}

func TestNothingIsAppliedWhenTheStoreRefusesTheCombinedWrite(t *testing.T) {
	repo := &savingRepo{mutableRepo: &mutableRepo{conns: map[string]sources.Connection{}}, fail: errors.New("disk full")}
	svc, rig := newConnectionsRig(t, repo, hexKeyA)
	_, err := svc.SaveConnection(context.Background(), "admin", sources.Connection{Slug: "plex", URL: "https://plex.example.test"}, map[secrets.Name]string{secrets.PlexToken: plexTokenProbe})
	if err == nil || rig.built != 0 || rig.m.Active("plex") || len(repo.conns) != 0 {
		t.Fatalf("err=%v built=%d conns=%v", err, rig.built, repo.conns)
	}
}

func TestAnInvalidCredentialLeavesTheAddressUnsaved(t *testing.T) {
	repo := &savingRepo{mutableRepo: &mutableRepo{conns: map[string]sources.Connection{}}}
	svc, rig := newConnectionsRig(t, repo, hexKeyA)
	_, err := svc.SaveConnection(context.Background(), "admin", sources.Connection{Slug: "plex", URL: "https://plex.example.test"}, map[secrets.Name]string{secrets.PlexToken: " padded "})
	if !errors.Is(err, secrets.ErrInvalidValue) || repo.saves != 0 || rig.built != 0 {
		t.Fatalf("err=%v saves=%d built=%d", err, repo.saves, rig.built)
	}
}

func TestACredentialThatDoesNotBelongToTheSourceIsRefused(t *testing.T) {
	repo := &savingRepo{mutableRepo: &mutableRepo{conns: map[string]sources.Connection{}}}
	svc, _ := newConnectionsRig(t, repo, hexKeyA)
	_, err := svc.SaveConnection(context.Background(), "admin", sources.Connection{Slug: "plex", URL: "https://plex.example.test"}, map[secrets.Name]string{secrets.RommToken: "x"})
	if !errors.Is(err, httpapi.ErrUnknownCredential) || repo.saves != 0 {
		t.Fatalf("err=%v saves=%d", err, repo.saves)
	}
}

func TestCredentialsCannotBeSavedWithoutAMasterKey(t *testing.T) {
	repo := &savingRepo{mutableRepo: &mutableRepo{conns: map[string]sources.Connection{}}}
	svc, _ := newConnectionsRig(t, repo, "")
	_, err := svc.SaveConnection(context.Background(), "admin", sources.Connection{Slug: "plex", URL: "https://plex.example.test"}, map[secrets.Name]string{secrets.PlexToken: plexTokenProbe})
	if !errors.Is(err, secrets.ErrNoMasterKey) || repo.saves != 0 {
		t.Fatalf("err=%v saves=%d", err, repo.saves)
	}
}

func TestNoCredentialKeepsTheSavedOne(t *testing.T) {
	repo := &savingRepo{mutableRepo: &mutableRepo{conns: map[string]sources.Connection{}, rows: sealedPlex(t, "old-token")}}
	svc, rig := newConnectionsRig(t, repo, hexKeyA)
	before := string(repo.rows[0].Ciphertext)
	got, err := svc.SaveConnection(context.Background(), "admin", sources.Connection{Slug: "plex", URL: "https://plex.example.test"}, nil)
	if err != nil || len(repo.rows) != 1 || string(repo.rows[0].Ciphertext) != before || !got.Active || rig.built != 1 {
		t.Fatalf("err=%v rows=%d active=%v built=%d", err, len(repo.rows), got.Active, rig.built)
	}
}

func TestAnEnvironmentAddressStaysReadOnlyWhileTheCredentialIsSaved(t *testing.T) {
	repo := &savingRepo{mutableRepo: &mutableRepo{conns: map[string]sources.Connection{}}}
	svc, rig := newConnectionsRig(t, repo, hexKeyA)
	svc.envCfg.PlexURL = "https://env.example.test"
	rig.m.envCfg.PlexURL = "https://env.example.test"
	// Sending an address is still refused, and nothing (not even a credential) is written.
	_, err := svc.SaveConnection(context.Background(), "admin", sources.Connection{Slug: "plex", URL: "https://other.example.test"}, map[secrets.Name]string{secrets.PlexToken: plexTokenProbe})
	if !errors.Is(err, httpapi.ErrSetByEnvironment) || repo.saves != 0 {
		t.Fatalf("err=%v saves=%d", err, repo.saves)
	}
	// A credential alone is saved and applied.
	got, err := svc.SaveConnection(context.Background(), "admin", sources.Connection{Slug: "plex"}, map[secrets.Name]string{secrets.PlexToken: plexTokenProbe})
	if err != nil || len(repo.conns) != 0 || !got.Active || !got.URLFromEnvironment || rig.built != 1 {
		t.Fatalf("err=%v conns=%v view=%+v built=%d", err, repo.conns, got, rig.built)
	}
}
