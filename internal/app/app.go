package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/devlogs"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/httpapi"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/inventory/audiobookshelf"
	"github.com/evilperson1337/requestarr/internal/inventory/jellyfin"
	"github.com/evilperson1337/requestarr/internal/inventory/navidrome"
	"github.com/evilperson1337/requestarr/internal/inventory/plex"
	"github.com/evilperson1337/requestarr/internal/inventory/romm"
	"github.com/evilperson1337/requestarr/internal/issues"
	"github.com/evilperson1337/requestarr/internal/jobs"
	"github.com/evilperson1337/requestarr/internal/notifications"
	"github.com/evilperson1337/requestarr/internal/notifications/apprise"
	"github.com/evilperson1337/requestarr/internal/opstatus"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/audible"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/igdb"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/musicbrainz"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/tmdb"
	"github.com/evilperson1337/requestarr/internal/requests"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
	"github.com/evilperson1337/requestarr/internal/sources"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
)

const startupTimeout = 30 * time.Second

// sourceSyncInterval is how often each Source syncs. It is also the retry interval: a failed run waits for the next tick.
const sourceSyncInterval = 15 * time.Minute

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger, build buildinfo.Info) error {
	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	logger.Info("connecting to database", "url", postgres.Redact(cfg.DatabaseURL))
	store, err := postgres.Open(startupCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return fmt.Errorf("database startup: %w", err)
	}
	defer store.Close()
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancelWorkers(); workers.Wait() }()
	logger.Info("database ready")
	envCfg := cfg
	boot, err := resolveBoot(ctx, cfg, store, logger)
	if err != nil {
		return err
	}
	cfg = boot.Config
	secretsService := boot.Secrets
	provider, err := oidc.New(ctx, oidc.Config{Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret}, cfg.PublicURL+"/api/v1/auth/callback")
	if err != nil {
		return fmt.Errorf("initialize OIDC provider: %w", err)
	}
	authHandler := &auth.Handler{Repo: store, Provider: provider, Issuer: cfg.OIDCIssuer, PublicURL: cfg.PublicURL, Secret: cfg.SessionHMACSecret, Secure: strings.HasPrefix(cfg.PublicURL, "https://")}
	// One IGDB provider serves links and searches, so they share its token and its request cap.
	igdbProvider := igdb.New(cfg.IGDBClientID, cfg.IGDBClientSecret, nil)
	metadataService := metadata.NewService(store, tmdb.New(cfg.TMDBReadToken, nil), musicbrainz.New(nil), igdbProvider, audible.New(domain.AudibleRegion(cfg.AudibleRegion), nil))
	// A search type exists only when its provider is configured; it is the one switch.
	searchers := map[string]metadata.Searcher{}
	if cfg.TMDBReadToken != "" {
		searchers["movie_tv"] = tmdb.New(cfg.TMDBReadToken, nil)
	}
	// MusicBrainz needs no key, so album and song search are always on. It shares the
	// resolver's one-per-second spacer inside the package.
	mbSearch := musicbrainz.New(nil)
	searchers["album"] = mbSearch
	searchers["song"] = mbSearch.SongSearcher()
	if cfg.IGDBClientID != "" && cfg.IGDBClientSecret != "" {
		searchers["game"] = igdbProvider
	}
	searchService := metadata.NewSearchService(searchers)
	if cfg.AppriseURL != "" {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := notifications.RunWorker(workerCtx, store, apprise.New(cfg.AppriseURL, cfg.AppriseConfigKey)); err != nil && workerCtx.Err() == nil {
				logger.Error("Apprise notification worker stopped", "error", err)
			}
		}()
	}
	runner := jobs.NewRunner(store)
	runner.Logger = logger
	aliaser := musicbrainz.NewAliaser(nil, musicbrainz.DefaultMinInterval)
	adapters := Adapters{
		SyncStore: store,
		Logger:    logger,
		NewSource: func(cfg config.Config, kind sources.Kind) (inventory.Source, sourceDecorator, error) {
			return newInventorySource(cfg, kind, func(ctx context.Context) { refreshMusicAliases(ctx, logger, store, aliaser) })
		},
	}
	rt, err := Build(boot, adapters)
	if err != nil {
		return err
	}
	syncer := newSourceSyncer(workerCtx, runner, &workers, map[string]syncSource{})
	ops := opstatus.New(rt.Providers, rt.Sources, store)
	manager := &sourceManager{
		ctx: workerCtx, workers: &workers, runner: runner, logger: logger, envCfg: envCfg, store: store, adapters: adapters,
		syncer: syncer, ops: ops, secrets: secretsService, running: map[string]*running{},
	}
	manager.adopt(rt.Jobs)
	setupRuntime := rt.Setup
	var logs *devlogs.Recorder
	if cfg.DevelopmentLogs { // cfg.Validate above rejects this outside development.
		logs = &devlogs.Recorder{}
	}
	checker := checks.New(checks.Config{Diagnostics: logs, Runtime: setupRuntime, OIDCClientSecret: cfg.OIDCClientSecret, Secrets: secretsService})
	setupService := setup.NewService(store, checker, setupRuntime)
	connections := &connectionsService{store: store, boot: store, envCfg: envCfg, manager: manager, secrets: secretsService, checker: checker, logger: logger}

	handler, err := httpapi.NewRouter(httpapi.Deps{
		Logger: logger, Build: build, Readiness: store, Auth: authHandler,
		Requests: requests.New(store), Resolver: metadataService, Inbox: store, MatchReview: store,
		MoviePreview: store,
		JobRuns:      store, Status: ops, Users: store, SetupState: store, Syncer: syncer,
		Wizard: setupService, Secrets: reloadingSecrets{Service: secretsService, manager: manager, logger: logger}, Connections: connections, Library: &libraryService{store: store, manager: manager}, Search: searchService, SearchLocal: store, Issues: httpapi.NewIssueService(issues.New(store.Issues())), DevelopmentLogs: logs,
	})
	if err != nil {
		return fmt.Errorf("initialize HTTP API: %w", err)
	}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("requestarr listening", "addr", cfg.HTTPAddr, "version", build.Version)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

type sourceSyncStore interface {
	PersistSnapshot(context.Context, inventory.Descriptor, string, inventory.Snapshot) error
	RecordSyncFailure(context.Context, inventory.Descriptor, time.Time, string) error
}

// sourceSyncJob is one sync of any Source as a runner job. Errors it returns
// are stored in job_runs and shown to administrators, so they are fixed
// strings: provider and database errors can carry URLs, keys or connection
// strings. A failed or incomplete enumeration records health only; it never
// adds or removes inventory (D015).
func sourceSyncJob(logger *slog.Logger, db sourceSyncStore, src inventory.Source, baseURL string) func(context.Context) error {
	return func(ctx context.Context) error {
		d := src.Descriptor()
		started := time.Now().UTC()
		snap, err := src.Snapshot(ctx)
		if err == nil && !snap.Complete {
			err = errInventoryRequest
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// The stored error stays a fixed string, but the operator's log says why. Adapter
			// errors are fixed phrases that never carry provider text (inventorytest checks).
			if !errors.Is(err, errInventoryRequest) {
				logger.Warn("source snapshot failed", "source", d.Kind, "reason", err.Error())
			}
			if e := db.RecordSyncFailure(ctx, d, started, errInventoryRequest.Error()); e != nil {
				logger.Warn("record sync health failed", "source", d.Kind)
			}
			return errInventoryRequest
		}
		if err := db.PersistSnapshot(ctx, d, baseURL, snap); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errSnapshotPersistence
		}
		return nil
	}
}

var (
	errInventoryRequest    = errors.New("inventory request failed")
	errSnapshotPersistence = errors.New("snapshot persistence failed")
)

// syncSource is one Source's sync job and its stable ID.
type syncSource struct {
	id  string
	job func(context.Context) error
}

// sourceSyncer starts manual syncs through the same runner and jobs the
// schedule uses, so manual and scheduled runs share one no-overlap guard.
type sourceSyncer struct {
	ctx     context.Context
	runner  *jobs.Runner
	workers *sync.WaitGroup

	mu      sync.RWMutex
	sources map[string]syncSource
}

func (s *sourceSyncer) set(slug string, source syncSource) {
	s.mu.Lock()
	s.sources[slug] = source
	s.mu.Unlock()
}

func (s *sourceSyncer) remove(slug string) {
	s.mu.Lock()
	delete(s.sources, slug)
	s.mu.Unlock()
}

// snapshot copies the current Sources for operational status.
func (s *sourceSyncer) snapshot() map[string]syncSource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]syncSource, len(s.sources))
	for slug, source := range s.sources {
		out[slug] = source
	}
	return out
}

// newSourceSyncer's ctx outlives any request: it bounds manual runs, which
// shutdown cancels and waits for through workers.
func newSourceSyncer(ctx context.Context, runner *jobs.Runner, workers *sync.WaitGroup, sources map[string]syncSource) *sourceSyncer {
	return &sourceSyncer{ctx: ctx, runner: runner, workers: workers, sources: sources}
}

func (s *sourceSyncer) SyncNow(_ context.Context, slug string) (jobs.Run, error) {
	s.mu.RLock()
	source, ok := s.sources[slug]
	s.mu.RUnlock()
	if !ok {
		return jobs.Run{}, jobs.ErrUnknownSource
	}
	// Register before starting so shutdown's Wait can never miss the run.
	s.workers.Add(1)
	run, done, err := s.runner.Start(s.ctx, source.id, jobs.TriggerManual, source.job)
	if err != nil {
		s.workers.Done()
		return jobs.Run{}, err
	}
	go func() { defer s.workers.Done(); <-done }()
	return run, nil
}

// maxAliasLookupsPerSync bounds the MusicBrainz calls one sync makes (each lookup
// takes at least one rate-limited request); the rest wait for the next sync.
const maxAliasLookupsPerSync = 20

type aliasStore interface {
	OpenCatalogItemsNeedingAliases(ctx context.Context, provider string, limit int) ([]domain.CatalogRef, error)
	SaveCatalogAliases(ctx context.Context, catalogID, kind string, values []string) error
	RecordAliasLookupFailure(ctx context.Context, catalogID string) error
}

type aliasLookup interface {
	Aliases(context.Context, domain.CatalogRef) ([]string, error)
}

// refreshMusicAliases asks MusicBrainz which release / release-track ids belong
// to each open music Request, so Plex's identifiers can be matched to it. It is
// best-effort: a failed lookup is logged and retried next sync, and never
// changes inventory or fails the sync (a missing alias only delays a match).
func refreshMusicAliases(ctx context.Context, logger *slog.Logger, store aliasStore, lookup aliasLookup) {
	pending, err := store.OpenCatalogItemsNeedingAliases(ctx, "musicbrainz", maxAliasLookupsPerSync)
	if err != nil {
		logger.Warn("list music requests needing aliases failed")
		return
	}
	for _, ref := range pending {
		if ctx.Err() != nil {
			return
		}
		values, err := lookup.Aliases(ctx, ref)
		if err != nil {
			logger.Warn("music alias lookup failed", "catalog_item", ref.ID)
			if e := store.RecordAliasLookupFailure(ctx, ref.ID); e != nil {
				logger.Warn("record alias lookup failure failed", "catalog_item", ref.ID)
			}
			continue
		}
		if err := store.SaveCatalogAliases(ctx, ref.ID, "mbid", values); err != nil {
			logger.Warn("save music aliases failed", "catalog_item", ref.ID)
		}
	}
}

// withAliasRefresh runs refresh before job so a sync can match music requested
// since the last one. refresh never returns an error, so job always runs.
func withAliasRefresh(refresh func(context.Context), job func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		refresh(ctx)
		return job(ctx)
	}
}

// newInventorySource builds the inventory client for a Source kind from the
// resolved configuration, with an optional decoration of its sync job. This is
// the one place a kind's constructor is named; it lives here, not in the
// registry, because the registry must not import provider clients.
func newInventorySource(cfg config.Config, kind sources.Kind, refreshAliases func(context.Context)) (inventory.Source, func(func(context.Context) error) func(context.Context) error, error) {
	url := *cfg.LibraryURL(kind.Slug)
	switch kind.Slug {
	case "jellyfin":
		client, err := jellyfin.New(url, cfg.JellyfinAPIKey, kind.ID)
		return client, nil, err
	case "plex":
		client, err := plex.New(url, cfg.PlexToken)
		return client, func(job func(context.Context) error) func(context.Context) error {
			return withAliasRefresh(refreshAliases, job)
		}, err
	case "romm":
		client, err := romm.New(url, cfg.RommToken)
		return client, nil, err
	case "navidrome":
		client, err := navidrome.New(url, cfg.NavidromeUsername, cfg.NavidromePassword)
		return client, nil, err
	case "audiobookshelf":
		client, err := audiobookshelf.New(url, cfg.AudiobookshelfAPIKey)
		return client, nil, err
	}
	return nil, nil, fmt.Errorf("no inventory client for Source kind %q", kind.Slug)
}

// sourceRefs lists every Source kind for operational status, with the sync
// job ID when it is configured and a note when its stored secret is unreadable.
func sourceRefs(syncSources map[string]syncSource, unreadable map[secrets.Name]string) []opstatus.SourceRef {
	var refs []opstatus.SourceRef
	for _, kind := range sources.All() {
		ref := opstatus.SourceRef{Slug: kind.Slug, Name: kind.Name, ID: syncSources[kind.Slug].id}
		for _, credential := range kind.Credentials {
			if note := unreadable[secrets.Name(credential.Secret)]; note != "" {
				ref.Note = note
			}
		}
		refs = append(refs, ref)
	}
	return refs
}
