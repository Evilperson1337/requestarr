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

	"github.com/example/requestarr/internal/auth"
	"github.com/example/requestarr/internal/auth/oidc"
	"github.com/example/requestarr/internal/buildinfo"
	"github.com/example/requestarr/internal/config"
	"github.com/example/requestarr/internal/httpapi"
	"github.com/example/requestarr/internal/inventory"
	"github.com/example/requestarr/internal/inventory/jellyfin"
	"github.com/example/requestarr/internal/notifications"
	"github.com/example/requestarr/internal/notifications/apprise"
	"github.com/example/requestarr/internal/providers/metadata"
	"github.com/example/requestarr/internal/providers/metadata/igdb"
	"github.com/example/requestarr/internal/providers/metadata/musicbrainz"
	"github.com/example/requestarr/internal/providers/metadata/tmdb"
	"github.com/example/requestarr/internal/store/postgres"
)

const startupTimeout = 30 * time.Second
const jellyfinSourceID = "5efcc577-5236-4ce3-b2c0-b9d58c64a718"

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
	if cfg.JellyfinURL != "" {
		client, err := jellyfin.New(cfg.JellyfinURL, cfg.JellyfinAPIKey, jellyfinSourceID)
		if err != nil {
			return fmt.Errorf("initialize Jellyfin integration: %w", err)
		}
		workers.Add(1)
		go func() { defer workers.Done(); syncJellyfin(workerCtx, logger, store, client, cfg.JellyfinURL) }()
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewWithMetadata(logger, build, store, metadataService, authHandler),
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

type jellyfinSyncStore interface {
	PersistJellyfinSnapshot(context.Context, string, inventory.Snapshot) error
	RecordJellyfinFailure(context.Context, time.Time, string) error
}

type jellyfinSnapshotter interface {
	Snapshot(context.Context) ([]inventory.Item, error)
}

func syncJellyfin(ctx context.Context, logger *slog.Logger, db jellyfinSyncStore, client jellyfinSnapshotter, baseURL string) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		started := time.Now().UTC()
		items, err := client.Snapshot(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warn("Jellyfin inventory sync failed", "error", "inventory request failed")
			if e := db.RecordJellyfinFailure(ctx, started, "inventory request failed"); e != nil {
				logger.Warn("record Jellyfin sync health failed")
			}
		} else {
			snap := inventory.Snapshot{SourceID: jellyfinSourceID, Items: items, Complete: true, ObservedAt: time.Now().UTC()}
			if e := db.PersistJellyfinSnapshot(ctx, baseURL, snap); e != nil {
				logger.Warn("persist Jellyfin inventory failed", "error", "snapshot persistence failed")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
