package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/requestarr/internal/buildinfo"
	"github.com/example/requestarr/internal/config"
	"github.com/example/requestarr/internal/httpapi"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger, build buildinfo.Info) error {
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(logger, build),
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
