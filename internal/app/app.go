package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
	"git.example.com/evilperson1337/Requestarr/internal/auth/oidc"
	"git.example.com/evilperson1337/Requestarr/internal/buildinfo"
	"git.example.com/evilperson1337/Requestarr/internal/config"
	"git.example.com/evilperson1337/Requestarr/internal/httpapi"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
	"git.example.com/evilperson1337/Requestarr/internal/inventory/audiobookshelf"
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
	authRepository := postgres.NewAuthRepository(pool)
	runtimeSecrets := postgres.NewRuntimeSecretReader(secretRepository, cipher)
	runtimeAuth := auth.NewRuntimeConfigurationService(authRepository, runtimeSecrets)
	if err := verifyRuntimeAuthentication(ctx, runtimeAuth); err != nil {
		return errors.New("startup runtime authentication configuration failed")
	}
	readiness := &readinessChecker{database: pool}
	readiness.migrationsComplete.Store(true)
	setupStatus := setup.NewService(postgres.NewSetupStatusRepository(pool))
	setupToken, setupConfigured := cfg.SetupToken()
	setupBootstrap := setup.NewBootstrapService(postgres.NewSetupBootstrapRepository(pool), setupToken, setupConfigured).
		WithCleanupFailureHandler(func() {
			logger.Warn("setup cleanup failed", "category", "persistence_unavailable")
		})
	setupWorkflowRepository := postgres.NewSetupWorkflowRepository(pool, secretRepository, cipher)
	setupWorkflow := setup.NewWorkflowService(setupWorkflowRepository, setupToken, setupConfigured)
	sourceConfigurations := postgres.NewSourceConfigurationRepository(pool, secretRepository)
	manualSync := inventory.NewManualSyncService(postgres.NewManualSyncRepository(pool))
	audiobookshelfProbe := audiobookshelf.NewProbe(audiobookshelf.ProbeOptions{AllowedCIDRs: cfg.ABSDestinationCIDRs()})
	discovery := oidc.NewDiscoveryClient(oidc.Options{
		AllowedCIDRs: cfg.OIDCDestinationCIDRs(),
		Development:  strings.EqualFold(cfg.Environment, "development"),
	})
	var browserAuth *auth.BrowserService
	var loginCookies *auth.LoginCookieCodec
	if encryptionStatus == config.EncryptionKeyReady {
		loginCookies, _ = auth.NewLoginCookieCodec(encryptionKey)
		browserAuth = auth.NewBrowserService(authRepository, oidc.NewLiveClient(discovery, runtimeSecrets))
	}
	authenticationValidation := setup.NewAuthenticationValidationService(setupWorkflowRepository, discovery, setupToken, setupConfigured)
	authenticationActivation := setup.NewActivationService(setupWorkflowRepository, setupToken, setupConfigured)
	publicOrigin, err := setup.NormalizeOrigin(cfg.PublicURL)
	if err != nil {
		return errors.New("startup public URL policy invalid")
	}

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.New(logger, build, readiness, setupStatus, httpapi.SetupSessionOptions{
			Exchanger:               setupBootstrap,
			Workflow:                setupWorkflow,
			AuthenticationValidator: authenticationValidation,
			AuthenticationActivator: authenticationActivation,
			PublicOrigin:            publicOrigin,
			SecureCookie:            cfg.SetupCookieSecure(),
			Auth:                    httpapi.AuthOptions{Runtime: runtimeAuth, Browser: browserAuth, Cookies: loginCookies, PublicOrigin: publicOrigin, SecureCookie: cfg.SetupCookieSecure()},
			AudiobookshelfSources:   httpapi.AudiobookshelfSourceOptions{Query: sourceConfigurations, Save: sourceConfigurations, ManualSync: manualSync, Probe: audiobookshelfProbe, Cipher: cipher, Auth: httpapi.AuthOptions{Runtime: runtimeAuth, Browser: browserAuth, Cookies: loginCookies, PublicOrigin: publicOrigin, SecureCookie: cfg.SetupCookieSecure()}},
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	// Durable workers start only after a handler registry is available. Starting
	// claimers without one would lease queued future work with no safe executor.
	var jobRunner jobLifecycle = noopJobLifecycle{}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("requestarr listening", "addr", cfg.HTTPAddr, "version", build.Version)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return shutdown(server, jobRunner, shutdownCtx)
	case err := <-errCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if shutdownErr := shutdown(server, jobRunner, shutdownCtx); shutdownErr != nil {
			return shutdownErr
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

type jobLifecycle interface {
	Start(context.Context) error
	Stop(context.Context) error
}

type noopJobLifecycle struct{}

func (noopJobLifecycle) Start(context.Context) error { return nil }
func (noopJobLifecycle) Stop(context.Context) error  { return nil }

type httpLifecycle interface {
	Shutdown(context.Context) error
}

// shutdown stops job claimers before closing the HTTP server, preventing new
// local work while allowing the bounded shutdown context to join workers.
func shutdown(server httpLifecycle, runner jobLifecycle, ctx context.Context) error {
	runnerErr := runner.Stop(ctx)
	serverErr := server.Shutdown(ctx)
	return errors.Join(runnerErr, serverErr)
}

func verifyRuntimeAuthentication(ctx context.Context, service *auth.RuntimeConfigurationService) error {
	_, err := service.VerifyActivated(ctx)
	if errors.Is(err, auth.ErrRuntimeConfigNotActivated) {
		return nil
	}
	return err
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
