package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/requestarr/internal/buildinfo"
	"github.com/example/requestarr/internal/config"
	"github.com/example/requestarr/internal/httpapi"
	"github.com/example/requestarr/internal/store/postgres"
)

const startupTimeout = 30 * time.Second

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger, build buildinfo.Info) error {
	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	logger.Info("connecting to database", "url", postgres.Redact(cfg.DatabaseURL))
	store, err := postgres.Open(startupCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return fmt.Errorf("database startup: %w", err)
	}
	defer store.Close()
	logger.Info("database ready")

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(logger, build, store),
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
