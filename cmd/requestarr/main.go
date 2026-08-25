package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"git.example.com/evilperson1337/Requestarr/internal/app"
	"git.example.com/evilperson1337/Requestarr/internal/buildinfo"
	"git.example.com/evilperson1337/Requestarr/internal/config"
	"git.example.com/evilperson1337/Requestarr/internal/maintenance/migrationrecovery"
	"git.example.com/evilperson1337/Requestarr/internal/store/postgres/migrations"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "maintenance" {
		if err := runMaintenance(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "requestarr: maintenance failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg, logger, buildinfo.Info{Version: version, Commit: commit, Date: date}); err != nil {
		logger.Error("requestarr stopped", "error", err)
		os.Exit(1)
	}
}

func runMaintenance(args []string) error {
	request, err := migrationrecovery.ParseCommand(args)
	if err != nil {
		return err
	}

	cfg := config.FromEnv()
	if err := cfg.ValidateMaintenanceDatabase(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return migrationrecovery.Execute(ctx, request, cfg.DatabaseURL(), migrations.RecoverDirtyV20)
}
