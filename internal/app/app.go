package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/buildinfo"
	"git.example.com/evilperson1337/Requestarr/internal/config"
	"git.example.com/evilperson1337/Requestarr/internal/httpapi"
	"git.example.com/evilperson1337/Requestarr/internal/secrets"
	"git.example.com/evilperson1337/Requestarr/internal/setup"
	"git.example.com/evilperson1337/Requestarr/internal/store/postgres"
	postgresmigrations "git.example.com/evilperson1337/Requestarr/internal/store/postgres/migrations"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger, build buildinfo.Info) error {
	pool, err := postgres.Open(ctx, cfg.DatabaseURL())
	if err != nil {
		return errors.New("startup database connection failed")
	}
	defer pool.Close()

	if err := postgresmigrations.Up(ctx, cfg.DatabaseURL()); err != nil {
		return errors.New("startup database migration failed")
	}
	encryptionKey, encryptionStatus := cfg.EncryptionKey()
	if encryptionStatus == config.EncryptionKeyInvalid {
		return errors.New("startup encrypted secret key validation failed")
	}
	var activeKeyID *secrets.KeyID
	var cipher *secrets.Cipher
	if encryptionStatus == config.EncryptionKeyReady {
		cipher = secrets.New(encryptionKey)
		keyID := cipher.KeyID()
		activeKeyID = &keyID
	}
	secretRepository := postgres.NewEncryptedSecretRepository(pool, activeKeyID)
	if err := secretRepository.EstablishActiveKey(ctx); err != nil {
		return errors.New("startup encrypted secret key validation failed")
	}
	readiness := &readinessChecker{database: pool}
	readiness.migrationsComplete.Store(true)
	setupStatus := setup.NewService(postgres.NewSetupStatusRepository(pool))
	setupToken, setupConfigured := cfg.SetupToken()
	setupBootstrap := setup.NewBootstrapService(postgres.NewSetupBootstrapRepository(pool), setupToken, setupConfigured).
		WithCleanupFailureHandler(func() {
			logger.Warn("setup cleanup failed", "category", "persistence_unavailable")
		})
	setupWorkflow := setup.NewWorkflowService(postgres.NewSetupWorkflowRepository(pool, secretRepository, cipher), setupToken, setupConfigured)
	publicOrigin, err := setup.NormalizeOrigin(cfg.PublicURL)
	if err != nil {
		return errors.New("startup public URL policy invalid")
	}

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.New(logger, build, readiness, setupStatus, httpapi.SetupSessionOptions{
			Exchanger:    setupBootstrap,
			Workflow:     setupWorkflow,
			PublicOrigin: publicOrigin,
			SecureCookie: cfg.SetupCookieSecure(),
		}),
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

type databaseReadiness interface {
	Ready(context.Context) error
}

type readinessChecker struct {
	migrationsComplete atomic.Bool
	database           databaseReadiness
}

func (r *readinessChecker) Ready(ctx context.Context) error {
	if r == nil || !r.migrationsComplete.Load() || r.database == nil {
		return errors.New("database initialization incomplete")
	}
	return r.database.Ready(ctx)
}
