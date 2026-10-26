package secrets_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/secrets"
)

type memRepo struct {
	rows    map[secrets.Name]secrets.Stored
	audit   []string
	clock   time.Time
	listErr error
}

func newMemRepo() *memRepo {
	return &memRepo{rows: map[secrets.Name]secrets.Stored{}, clock: time.Unix(1000, 0)}
}

func (m *memRepo) ListStoredSecrets(context.Context) ([]secrets.Stored, error) {
	var out []secrets.Stored
	for _, r := range m.rows {
		out = append(out, r)
	}
	return out, m.listErr
}

func (m *memRepo) PutStoredSecret(_ context.Context, actor string, name secrets.Name, s secrets.Sealed) error {
	m.clock = m.clock.Add(time.Second)
	m.rows[name] = secrets.Stored{Name: name, Sealed: s, UpdatedAt: m.clock}
	m.audit = append(m.audit, actor+" set "+string(name))
	return nil
}

func (m *memRepo) DeleteStoredSecret(_ context.Context, actor string, name secrets.Name) error {
	delete(m.rows, name)
	m.audit = append(m.audit, actor+" clear "+string(name))
	return nil
}

func sealer(t *testing.T, b byte) secrets.Sealer {
	t.Helper()
	s, err := secrets.NewAESGCM(key32(b))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func env(v string) map[secrets.Name]string {
	if v == "" {
		return nil
	}
	return map[secrets.Name]string{secrets.TMDBReadToken: v}
}

const tmdb = secrets.TMDBReadToken

func TestResolutionTable(t *testing.T) {
	ctx := context.Background()
	good, other := sealer(t, 1), sealer(t, 2)
	stored := func(s secrets.Sealer) *memRepo {
		r := newMemRepo()
		sealed, _ := s.Seal(tmdb, []byte("stored-value"))
		r.rows[tmdb] = secrets.Stored{Name: tmdb, Sealed: sealed, UpdatedAt: r.clock}
		return r
	}
	cases := []struct {
		name       string
		repo       *memRepo
		sealer     secrets.Sealer
		env        string
		wantValue  string
		wantSource secrets.Source
	}{
		{"nothing", newMemRepo(), good, "", "", secrets.SourceNone},
		{"env only", newMemRepo(), good, "env-value", "env-value", secrets.SourceEnvironment},
		{"stored only", stored(good), good, "", "stored-value", secrets.SourceStored},
		{"env wins and shadows stored", stored(good), good, "env-value", "env-value", secrets.SourceShadowed},
		{"wrong key is unreadable not unconfigured", stored(other), good, "", "", secrets.SourceUnreadable},
		{"missing master key is unreadable", stored(good), nil, "", "", secrets.SourceUnreadable},
		{"env still wins when stored is unreadable", stored(other), good, "env-value", "env-value", secrets.SourceShadowed},
		{"no master key, env only", newMemRepo(), nil, "env-value", "env-value", secrets.SourceEnvironment},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := secrets.NewService(tc.repo, tc.sealer, env(tc.env))
			got, err := svc.Resolve(ctx, tmdb)
			if err != nil {
				t.Fatal(err)
			}
			if got.Value != tc.wantValue || got.Source != tc.wantSource {
				t.Fatalf("got source %q (value present %v), want %q", got.Source, got.Value != "", tc.wantSource)
			}
		})
	}
}

func TestSourceLabelsMatchD024(t *testing.T) {
	want := map[secrets.Source]string{secrets.SourceEnvironment: "environment", secrets.SourceStored: "stored", secrets.SourceShadowed: "stored (shadowed by environment)"}
	for s, label := range want {
		if string(s) != label {
			t.Errorf("%q != %q", s, label)
		}
	}
}

func TestSetStoresEncryptedAndNeverReturnsTheValue(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := secrets.NewService(repo, sealer(t, 1), nil)
	if err := svc.Set(ctx, "admin-1", tmdb, "super-secret-token"); err != nil {
		t.Fatal(err)
	}
	row := repo.rows[tmdb]
	if strings.Contains(string(row.Ciphertext), "super-secret-token") || row.KeyID == "" {
		t.Fatal("secret stored in the clear or without key id")
	}
	if strings.Join(repo.audit, ",") != "admin-1 set tmdb_read_token" {
		t.Fatalf("audit %v", repo.audit)
	}
	statuses, enabled, err := svc.Status(ctx)
	if err != nil || !enabled || len(statuses) != len(secrets.All()) {
		t.Fatalf("statuses %v enabled %v err %v", statuses, enabled, err)
	}
	st := statuses[0]
	if !st.Configured || st.Source != secrets.SourceStored || st.Unreadable {
		t.Fatalf("status %+v", st)
	}
	if got, _ := svc.Resolve(ctx, tmdb); got.Value != "super-secret-token" {
		t.Fatal("resolved value lost")
	}
}

func TestSetRefusedWithoutMasterKeyAndForBadInput(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	if err := secrets.NewService(repo, nil, nil).Set(ctx, "a", tmdb, "v"); !errors.Is(err, secrets.ErrNoMasterKey) {
		t.Fatalf("no master key: %v", err)
	}
	svc := secrets.NewService(repo, sealer(t, 1), nil)
	if err := svc.Set(ctx, "a", "nope", "v"); !errors.Is(err, secrets.ErrUnknownSecret) {
		t.Errorf("unknown name: %v", err)
	}
	for _, v := range []string{"", " padded ", "line\nbreak", strings.Repeat("x", 4097)} {
		if err := svc.Set(ctx, "a", tmdb, v); !errors.Is(err, secrets.ErrInvalidValue) {
			t.Errorf("value %q: %v", v, err)
		}
	}
	if len(repo.rows) != 0 || len(repo.audit) != 0 {
		t.Fatal("rejected input was persisted")
	}
}

func TestClearIsAvailableWithoutMasterKey(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	sealed, _ := sealer(t, 1).Seal(tmdb, []byte("v"))
	repo.rows[tmdb] = secrets.Stored{Name: tmdb, Sealed: sealed}
	svc := secrets.NewService(repo, nil, nil)
	if err := svc.Clear(ctx, "admin-1", tmdb); err != nil {
		t.Fatal(err)
	}
	if len(repo.rows) != 0 || repo.audit[0] != "admin-1 clear tmdb_read_token" {
		t.Fatalf("rows %v audit %v", repo.rows, repo.audit)
	}
}

func TestStatusReportsUnreadableAndShadowing(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	sealed, _ := sealer(t, 2).Seal(tmdb, []byte("v"))
	repo.rows[tmdb] = secrets.Stored{Name: tmdb, Sealed: sealed}
	st, _, _ := secrets.NewService(repo, sealer(t, 1), nil).Status(ctx)
	if !st[0].Unreadable || st[0].Configured || st[0].Source != secrets.SourceUnreadable {
		t.Fatalf("%+v", st[0])
	}
	st, _, _ = secrets.NewService(repo, sealer(t, 1), env("e")).Status(ctx)
	if st[0].Source != secrets.SourceShadowed || !st[0].Configured || st[0].Unreadable {
		t.Fatalf("%+v", st[0])
	}
}

func TestPendingRestartTracksTheValueNextBootWouldUse(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := secrets.NewService(repo, sealer(t, 1), nil)
	if _, err := svc.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	pending := func() bool {
		st, _, err := svc.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return st[0].PendingRestart
	}
	if pending() {
		t.Fatal("pending before any change")
	}
	_ = svc.Set(ctx, "a", tmdb, "one")
	if !pending() {
		t.Fatal("new stored secret not pending")
	}
	// A restart picks it up.
	svc2 := secrets.NewService(repo, sealer(t, 1), nil)
	if _, err := svc2.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	st, _, _ := svc2.Status(ctx)
	if st[0].PendingRestart {
		t.Fatal("pending right after boot")
	}
	_ = svc2.Set(ctx, "a", tmdb, "two")
	st, _, _ = svc2.Status(ctx)
	if !st[0].PendingRestart {
		t.Fatal("replacement not pending")
	}
	_ = svc2.Clear(ctx, "a", tmdb)
	st, _, _ = svc2.Status(ctx)
	if !st[0].PendingRestart {
		t.Fatal("clear of an active secret not pending")
	}
}

func TestBootReturnsEffectiveValuesAndSurvivesUnreadable(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	sealed, _ := sealer(t, 2).Seal(tmdb, []byte("v"))
	repo.rows[tmdb] = secrets.Stored{Name: tmdb, Sealed: sealed}
	values, err := secrets.NewService(repo, sealer(t, 1), nil).Boot(ctx)
	if err != nil {
		t.Fatalf("unreadable must not fail boot: %v", err)
	}
	if values[tmdb] != "" {
		t.Fatal("unreadable secret produced a value")
	}
}
