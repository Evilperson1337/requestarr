package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/httpapi"
	"github.com/evilperson1337/requestarr/internal/notifications"
	"github.com/evilperson1337/requestarr/internal/opstatus"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

// liveConfig is a stand-in for the resolved configuration that tests change
// between reloads, the way saving a credential changes what resolution returns.
type liveConfig struct {
	mu  sync.Mutex
	cfg config.Config
	err error
}

func (l *liveConfig) set(f func(*config.Config)) { l.mu.Lock(); f(&l.cfg); l.mu.Unlock() }
func (l *liveConfig) fail(err error)             { l.mu.Lock(); l.err = err; l.mu.Unlock() }
func (l *liveConfig) resolve(context.Context) (booted, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return booted{}, l.err
	}
	return booted{Config: l.cfg, Unreadable: map[secrets.Name]string{}}, nil
}

func newTestProviders(t *testing.T, cfg config.Config) (*providerSet, *liveConfig) {
	t.Helper()
	live := &liveConfig{cfg: cfg}
	return newProviderSet(cfg, nil, nil, live.resolve, nil), live
}

func TestSavingATMDBTokenOffersMoviesAndTVWithoutARestart(t *testing.T) {
	ps, live := newTestProviders(t, config.Config{})
	if slices.Contains(ps.Types(), "movie_tv") {
		t.Fatalf("movie_tv offered with no token: %v", ps.Types())
	}
	live.set(func(c *config.Config) { c.TMDBReadToken = "saved-tmdb-token" })
	if err := ps.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ps.Types(), "movie_tv") {
		t.Fatalf("movie_tv missing after save: %v", ps.Types())
	}
}

func TestClearingTheTokenWithNoEnvironmentValueRemovesTheType(t *testing.T) {
	ps, live := newTestProviders(t, config.Config{TMDBReadToken: "saved-tmdb-token"})
	if !slices.Contains(ps.Types(), "movie_tv") {
		t.Fatal("precondition: movie_tv offered")
	}
	live.set(func(c *config.Config) { c.TMDBReadToken = "" })
	if err := ps.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ps.Types(), "movie_tv") {
		t.Fatalf("movie_tv still offered after clear: %v", ps.Types())
	}
	if _, err := ps.Search(context.Background(), "movie_tv", "alien", 1, metadata.SearchOptions{}); !errors.Is(err, metadata.ErrNotConfigured) {
		t.Fatalf("search on a removed type: %v", err)
	}
}

func TestGamesNeedBothIGDBHalvesAndAppearWhenTheSecondArrives(t *testing.T) {
	ps, live := newTestProviders(t, config.Config{IGDBClientID: "client"})
	if slices.Contains(ps.Types(), "game") {
		t.Fatal("game offered with only the client ID")
	}
	live.set(func(c *config.Config) { c.IGDBClientSecret = "secret" })
	if err := ps.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ps.Types(), "game") {
		t.Fatalf("game missing: %v", ps.Types())
	}
}

func TestReportedProvidersFollowTheSwap(t *testing.T) {
	var seen atomic.Value
	live := &liveConfig{}
	ps := newProviderSet(config.Config{}, nil, nil, live.resolve, func(p []opstatus.Provider) { seen.Store(p) })
	live.set(func(c *config.Config) { c.TMDBReadToken = "t" })
	if err := ps.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := seen.Load().([]opstatus.Provider)
	for _, p := range got {
		if p.Key == "tmdb" && !p.Configured {
			t.Fatal("status still reports TMDB as not configured")
		}
	}
	if len(got) == 0 {
		t.Fatal("providers not published")
	}
}

func TestFailedReloadKeepsThePreviousProvidersAndLeaksNothing(t *testing.T) {
	ps, live := newTestProviders(t, config.Config{TMDBReadToken: "working-token"})
	live.set(func(c *config.Config) { c.TMDBReadToken = "" })
	live.fail(errors.New("invalid saved IGDB client ID leaky-value-123"))
	err := ps.Reload(context.Background())
	if err == nil {
		t.Fatal("reload should fail")
	}
	if !slices.Contains(ps.Types(), "movie_tv") {
		t.Fatalf("previous snapshot was lost: %v", ps.Types())
	}
	if strings.Contains(err.Error(), "leaky-value-123") || strings.Contains(err.Error(), "working-token") {
		t.Fatalf("error leaks a value: %v", err)
	}
	live.fail(nil)
	if err := ps.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ps.Types(), "movie_tv") {
		t.Fatal("a later good reload must apply")
	}
}

func TestReloadResolvesThroughTheRealConfigurationLayers(t *testing.T) {
	// Stored TMDB token applies; an environment Apprise key beats the stored one.
	repo := &mutableRepo{}
	env := config.Config{SecretsKey: hexKeyA, AppriseConfigKey: "env-key"}
	resolveNow := func(ctx context.Context) (booted, error) {
		return resolveConfig(ctx, env, repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	ps := newProviderSet(env, nil, nil, resolveNow, nil)
	if slices.Contains(ps.Types(), "movie_tv") {
		t.Fatal("precondition")
	}
	repo.mu.Lock()
	repo.rows = storedAs(t, hexKeyA, map[secrets.Name]string{secrets.TMDBReadToken: "stored-token", secrets.AppriseConfigKey: "stored-key"}).rows
	repo.mu.Unlock()
	if err := ps.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ps.Types(), "movie_tv") {
		t.Fatalf("stored token not applied: %v", ps.Types())
	}
	if got := ps.snapshot().appriseKey; got != "env-key" {
		t.Fatalf("environment must win over stored for the Apprise key, got %q", got)
	}
}

func TestAppriseKeyChangesApplyToTheNextSendAndNilMeansOff(t *testing.T) {
	var paths []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	ps, live := newTestProviders(t, config.Config{AppriseURL: srv.URL})
	if ps.Configured() {
		t.Fatal("Apprise with a URL but no key must be off")
	}
	if err := ps.Send(context.Background(), notifications.Message{Title: "t"}); err == nil {
		t.Fatal("send while off must fail, not pretend")
	}
	live.set(func(c *config.Config) { c.AppriseConfigKey = "first" })
	if err := ps.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ps.Send(context.Background(), notifications.Message{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	live.set(func(c *config.Config) { c.AppriseConfigKey = "second" })
	if err := ps.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ps.Send(context.Background(), notifications.Message{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	live.set(func(c *config.Config) { c.AppriseConfigKey = "" })
	if err := ps.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ps.Configured() {
		t.Fatal("clearing the key must switch notifications off")
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(paths, []string{"/notify/first", "/notify/second"}) {
		t.Fatalf("paths %v", paths)
	}
}

// numberedOutbox hands out events 1..total once each and records every completion.
type numberedOutbox struct {
	mu        sync.Mutex
	total     int
	next      int
	completed map[string]int
}

func (o *numberedOutbox) ClaimGlobalApprise(context.Context, time.Duration) (string, string, string, string, int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.next >= o.total {
		return "", "", "", "", 0, nil
	}
	o.next++
	id := strconv.Itoa(o.next)
	return id, "event " + id, "body", "lease-" + id, 1, nil
}
func (o *numberedOutbox) CompleteGlobalApprise(_ context.Context, id, _ string, ok bool, _ time.Duration) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.completed == nil {
		o.completed = map[string]int{}
	}
	if ok {
		o.completed[id]++
	}
	return nil
}
func (o *numberedOutbox) done() int { o.mu.Lock(); defer o.mu.Unlock(); return len(o.completed) }

func TestTheWorkerPicksUpANewKeyWithoutLosingOrDuplicatingEvents(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Title string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		hits[body.Title]++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	const total = 30
	ps, live := newTestProviders(t, config.Config{AppriseURL: srv.URL, AppriseConfigKey: "one"})
	outbox := &numberedOutbox{total: total}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- notifications.RunWorker(ctx, outbox, ps) }()
	for i := 0; outbox.done() < total && i < 3000; i++ {
		keys := []string{"two", "", "three"}
		live.set(func(c *config.Config) { c.AppriseConfigKey = keys[i%3] })
		_ = ps.Reload(context.Background())
		time.Sleep(time.Millisecond)
		if i == 20 {
			live.set(func(c *config.Config) { c.AppriseConfigKey = "final" })
			_ = ps.Reload(context.Background())
			break
		}
	}
	deadline := time.After(10 * time.Second)
	for outbox.done() < total {
		select {
		case <-deadline:
			t.Fatalf("only %d of %d events delivered", outbox.done(), total)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-finished
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i <= total; i++ {
		id := strconv.Itoa(i)
		if outbox.completed[id] != 1 || hits["event "+id] != 1 {
			t.Fatalf("event %s completed %d times, sent %d times", id, outbox.completed[id], hits["event "+id])
		}
	}
}

func TestConcurrentReloadsAreSerialized(t *testing.T) {
	var inFlight, peak atomic.Int32
	resolve := func(context.Context) (booted, error) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		defer inFlight.Add(-1)
		return booted{Config: config.Config{}, Unreadable: map[secrets.Name]string{}}, nil
	}
	ps := newProviderSet(config.Config{}, nil, nil, resolve, nil)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = ps.Reload(context.Background()) }()
	}
	wg.Wait()
	if peak.Load() != 1 {
		t.Fatalf("%d reloads overlapped; a later save could be overwritten by an earlier one", peak.Load())
	}
}

func TestReadersNeverSeeAPartiallyUpdatedSet(t *testing.T) {
	// Both halves flip together; a reader must see all of them on or all off.
	on := config.Config{TMDBReadToken: "t", IGDBClientID: "c", IGDBClientSecret: "s"}
	ps, live := newTestProviders(t, on)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var torn atomic.Int32
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				types := ps.Types()
				if slices.Contains(types, "movie_tv") != slices.Contains(types, "game") {
					torn.Add(1)
				}
				_, _ = ps.Search(context.Background(), "nope", "text", 1, metadata.SearchOptions{})
				_ = ps.Configured()
			}
		}()
	}
	for i := 0; i < 200; i++ {
		live.set(func(c *config.Config) {
			if i%2 == 0 {
				*c = config.Config{}
			} else {
				*c = on
			}
		})
		if err := ps.Reload(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if torn.Load() != 0 {
		t.Fatalf("%d reads saw a partially updated provider set", torn.Load())
	}
}

// recordingRepo keeps sealed rows in memory so Set / Clear really change what Status sees.
type recordingRepo struct {
	mu   sync.Mutex
	rows map[secrets.Name]secrets.Stored
}

func (r *recordingRepo) ListStoredSecrets(context.Context) ([]secrets.Stored, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []secrets.Stored
	for _, row := range r.rows {
		out = append(out, row)
	}
	return out, nil
}
func (r *recordingRepo) PutStoredSecret(_ context.Context, _ string, name secrets.Name, sealed secrets.Sealed) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rows == nil {
		r.rows = map[secrets.Name]secrets.Stored{}
	}
	r.rows[name] = secrets.Stored{Name: name, Sealed: sealed}
	return nil
}
func (r *recordingRepo) DeleteStoredSecret(_ context.Context, _ string, name secrets.Name) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, name)
	return nil
}

type stubReloader struct {
	err   error
	calls atomic.Int32
}

func (s *stubReloader) Reload(context.Context) error { s.calls.Add(1); return s.err }

func newReloadingSecrets(t *testing.T, reloader *stubReloader) (reloadingSecrets, *recordingRepo, *secrets.Service) {
	t.Helper()
	key, _ := secrets.ParseMasterKey(hexKeyA)
	sealer, _ := secrets.NewAESGCM(key)
	repo := &recordingRepo{}
	svc := secrets.NewService(repo, sealer, nil)
	if _, err := svc.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	return reloadingSecrets{Service: svc, providers: reloader, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, repo, svc
}

func pending(t *testing.T, svc *secrets.Service, name secrets.Name) bool {
	t.Helper()
	statuses, _, err := svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range statuses {
		if st.Name == name {
			return st.PendingRestart
		}
	}
	t.Fatalf("no status for %s", name)
	return false
}

func TestSavingAndClearingProviderSecretsReloadsAndIsNotPendingARestart(t *testing.T) {
	for _, name := range []secrets.Name{secrets.TMDBReadToken, secrets.IGDBClientSecret, secrets.AppriseConfigKey} {
		t.Run(string(name), func(t *testing.T) {
			reloader := &stubReloader{}
			rs, repo, svc := newReloadingSecrets(t, reloader)
			if err := rs.Set(context.Background(), "admin", name, "a-value"); err != nil {
				t.Fatal(err)
			}
			if reloader.calls.Load() != 1 || pending(t, svc, name) {
				t.Fatalf("calls=%d pending=%v after Set", reloader.calls.Load(), pending(t, svc, name))
			}
			if err := rs.Clear(context.Background(), "admin", name); err != nil {
				t.Fatal(err)
			}
			if reloader.calls.Load() != 2 || pending(t, svc, name) || len(repo.rows) != 0 {
				t.Fatalf("calls=%d after Clear", reloader.calls.Load())
			}
		})
	}
}

func TestAFailedReloadIsReportedAsNotAppliedAndKeepsTheStoredValue(t *testing.T) {
	reloader := &stubReloader{err: errors.New("boom hunter2-secret")}
	rs, repo, svc := newReloadingSecrets(t, reloader)
	err := rs.Set(context.Background(), "admin", secrets.TMDBReadToken, "hunter2-secret")
	if !errors.Is(err, httpapi.ErrNotApplied) {
		t.Fatalf("want ErrNotApplied, got %v", err)
	}
	if strings.Contains(err.Error(), "hunter2-secret") {
		t.Fatalf("error leaks the value: %v", err)
	}
	// Same semantics as SaveConnection: the write stands, the running set is unchanged.
	if len(repo.rows) != 1 {
		t.Fatal("the saved value should stay stored")
	}
	if !pending(t, svc, secrets.TMDBReadToken) {
		t.Fatal("a value that was not applied must still be reported as pending")
	}
}

func TestOtherSecretsDoNotRebuildTheProviders(t *testing.T) {
	reloader := &stubReloader{}
	rs, _, _ := newReloadingSecrets(t, reloader)
	// A library credential goes through the source manager only; with no manager
	// wired a call would panic, so use a name outside both groups.
	if err := rs.Set(context.Background(), "admin", secrets.Name("not_a_secret"), "v"); err == nil {
		t.Fatal("unknown secret should be rejected")
	}
	if reloader.calls.Load() != 0 {
		t.Fatal("providers rebuilt for an unrelated secret")
	}
}

func TestSavingTheIGDBClientIDReloadsAndAFailureIsNotApplied(t *testing.T) {
	reloader := &stubReloader{}
	store := &memoryIGDBStore{}
	svc := igdbSettingsService{store: store, providers: reloader}
	if err := svc.SetIGDBClientID(context.Background(), "client"); err != nil || reloader.calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, reloader.calls.Load())
	}
	reloader.err = errors.New("boom")
	if err := svc.SetIGDBClientID(context.Background(), "client-2"); !errors.Is(err, httpapi.ErrNotApplied) || store.id != "client-2" {
		t.Fatalf("err=%v stored=%q", err, store.id)
	}
	envOwned := igdbSettingsService{store: store, envID: "env", providers: reloader}
	before := reloader.calls.Load()
	if err := envOwned.SetIGDBClientID(context.Background(), "x"); !errors.Is(err, httpapi.ErrIGDBClientIDFromEnvironment) || reloader.calls.Load() != before {
		t.Fatalf("environment-owned ID must not reload: %v", err)
	}
}
