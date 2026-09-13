package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/requestarr/internal/app"
	"github.com/example/requestarr/internal/buildinfo"
	"github.com/example/requestarr/internal/config"
	"github.com/example/requestarr/internal/store/postgres"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var showVersion, migrateOnly bool
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&migrateOnly, "migrate-only", false, "apply pending database migrations then exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("requestarr %s (%s) built %s\n", version, commit, date)
		return
	}

	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "requestarr: invalid configuration: %v\n", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel()}))

	if migrateOnly {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store, err := postgres.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			logger.Error("migration failed", "error", err)
			os.Exit(1)
		}
		store.Close()
		logger.Info("migrations applied")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg, logger, buildinfo.Info{Version: version, Commit: commit, Date: date}); err != nil {
		logger.Error("requestarr stopped", "error", err)
		os.Exit(1)
	}
}
