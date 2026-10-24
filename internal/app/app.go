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
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/httpapi"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/inventory/jellyfin"
	"github.com/evilperson1337/requestarr/internal/inventory/navidrome"
	"github.com/evilperson1337/requestarr/internal/inventory/plex"
	"github.com/evilperson1337/requestarr/internal/inventory/romm"
	"github.com/evilperson1337/requestarr/internal/jobs"
	"github.com/evilperson1337/requestarr/internal/notifications"
	"github.com/evilperson1337/requestarr/internal/notifications/apprise"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/igdb"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/musicbrainz"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/tmdb"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
)

const startupTimeout = 30 * time.Second
const jellyfinSourceID = "5efcc577-5236-4ce3-b2c0-b9d58c64a718"

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
	provider, err := oidc.New(ctx, oidc.Config{Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret}, cfg.PublicURL+"/api/v1/auth/callback")
	if err != nil {
		return fmt.Errorf("initialize OIDC provider: %w", err)
	}
	authHandler := &auth.Handler{Repo: store, Provider: provider, Issuer: cfg.OIDCIssuer, PublicURL: cfg.PublicURL, Secret: cfg.SessionHMACSecret, Secure: strings.HasPrefix(cfg.PublicURL, "https://")}
	metadataService := metadata.NewService(store, tmdb.New(cfg.TMDBReadToken, nil), musicbrainz.New(nil), igdb.New(cfg.IGDBClientID, cfg.IGDBClientSecret, nil))
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
	syncSources := map[string]syncSource{}
	addSource := func(slug string, src inventory.Source, baseURL string, decorate func(func(context.Context) error) func(context.Context) error) {
		job := sourceSyncJob(logger, store, src, baseURL)
		if decorate != nil {
			job = decorate(job)
		}
		id := src.Descriptor().ID
		syncSources[slug] = syncSource{id: id, job: job}
		workers.Add(1)
		go func() {
			defer workers.Done()
			runner.Schedule(workerCtx, sourceSyncInterval, id, job)
		}()
	}
	if cfg.JellyfinURL != "" {
		client, err := jellyfin.New(cfg.JellyfinURL, cfg.JellyfinAPIKey, jellyfinSourceID)
		if err != nil {
			return fmt.Errorf("initialize Jellyfin integration: %w", err)
		}
		addSource("jellyfin", client, cfg.JellyfinURL, nil)
	}
	if cfg.PlexURL != "" {
		client, err := plex.New(cfg.PlexURL, cfg.PlexToken)
		if err != nil {
			return fmt.Errorf("initialize Plex integration: %w", err)
		}
		aliaser := musicbrainz.NewAliaser(nil, musicbrainz.DefaultMinInterval)
		addSource("plex", client, cfg.PlexURL, func(job func(context.Context) error) func(context.Context) error {
			return withAliasRefresh(func(ctx context.Context) { refreshMusicAliases(ctx, logger, store, aliaser) }, job)
		})
	}
	if cfg.RommURL != "" {
		client, err := romm.New(cfg.RommURL, cfg.RommToken)
		if err != nil {
			return fmt.Errorf("initialize RomM integration: %w", err)
		}
		addSource("romm", client, cfg.RommURL, nil)
	}
	if cfg.NavidromeURL != "" {
		client, err := navidrome.New(cfg.NavidromeURL, cfg.NavidromeUsername, cfg.NavidromePassword)
		if err != nil {
			return fmt.Errorf("initialize Navidrome integration: %w", err)
		}
		addSource("navidrome", client, cfg.NavidromeURL, nil)
	}
	syncer := newSourceSyncer(workerCtx, runner, &workers, syncSources)
	setupRuntime := setup.Runtime{PublicURL: cfg.PublicURL, OIDCIssuer: cfg.OIDCIssuer, OIDCClientID: cfg.OIDCClientID, IGDBClientID: cfg.IGDBClientID,
		OIDCSecretConfigured: cfg.OIDCClientSecret != "", TMDBTokenConfigured: cfg.TMDBReadToken != "", IGDBSecretConfigured: cfg.IGDBClientSecret != "", AppriseURL: cfg.AppriseURL, AppriseKeyConfigured: cfg.AppriseConfigKey != "", Development: cfg.Environment == "development",
		LibrarySecrets: map[string]bool{"jellyfin": cfg.JellyfinAPIKey != "", "plex": cfg.PlexToken != "", "romm": cfg.RommToken != "", "navidromeUsername": cfg.NavidromeUsername != "", "navidromePassword": cfg.NavidromePassword != ""}}
	setupRuntime.LibraryURLs = map[string]string{"jellyfin": cfg.JellyfinURL, "plex": cfg.PlexURL, "romm": cfg.RommURL, "navidrome": cfg.NavidromeURL}
	setupService := setup.NewService(store, checks.New(checks.Config{Runtime: setupRuntime, OIDCClientSecret: cfg.OIDCClientSecret, TMDBReadToken: cfg.TMDBReadToken, IGDBClientSecret: cfg.IGDBClientSecret,
		JellyfinAPIKey: cfg.JellyfinAPIKey, PlexToken: cfg.PlexToken, RommToken: cfg.RommToken, NavidromeUsername: cfg.NavidromeUsername, NavidromePassword: cfg.NavidromePassword}), setupRuntime)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewWithSetup(logger, build, store, metadataService, syncer, setupService, authHandler),
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
	sources map[string]syncSource
}

// newSourceSyncer's ctx outlives any request: it bounds manual runs, which
// shutdown cancels and waits for through workers.
func newSourceSyncer(ctx context.Context, runner *jobs.Runner, workers *sync.WaitGroup, sources map[string]syncSource) *sourceSyncer {
	return &sourceSyncer{ctx: ctx, runner: runner, workers: workers, sources: sources}
}

func (s *sourceSyncer) SyncNow(_ context.Context, slug string) (jobs.Run, error) {
	source, ok := s.sources[slug]
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
