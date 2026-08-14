package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/requestarr/internal/app"
	"github.com/example/requestarr/internal/buildinfo"
	"github.com/example/requestarr/internal/config"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
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
