package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/evilperson1337/requestarr/internal/app"
	"github.com/evilperson1337/requestarr/internal/auth/password"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "reset-admin-password" {
		if err := resetAdminPassword(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "requestarr: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(os.Args[2:], config.FromEnv().HTTPAddr); err != nil {
			fmt.Fprintf(os.Stderr, "requestarr: healthcheck: %v\n", err)
			os.Exit(1)
		}
		return
	}
	var showVersion, migrateOnly bool
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&migrateOnly, "migrate-only", false, "apply pending database migrations then exit")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "requestarr: unexpected command or argument; available commands: healthcheck, reset-admin-password")
		os.Exit(2)
	}

	if showVersion {
		fmt.Printf("requestarr %s (%s) built %s\n", version, commit, date)
		return
	}

	cfg := config.FromEnv()
	// The server run validates fully inside app.Run, after stored secrets are
	// resolved (D024); migrate-only has no stored secrets to wait for.
	validate := cfg.ValidateBootstrap
	if migrateOnly {
		validate = cfg.Validate
	}
	if err := validate(); err != nil {
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

// Shell access is the recovery gate. Only database configuration is needed.
func resetAdminPassword(args []string) error {
	fs := flag.NewFlagSet("reset-admin-password", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("reset-admin-password accepts no arguments")
	}
	cfg := config.FromEnv()
	if cfg.DatabaseURL == "" {
		return fmt.Errorf("database configuration required; set REQUESTARR_DATABASE_URL or complete REQUESTARR_DB_* settings")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database unavailable; check the database configuration")
	}
	defer store.Close()
	pw, err := password.Random()
	if err != nil {
		return fmt.Errorf("generate administrator password")
	}
	hash, err := password.Default.Hash(pw)
	if err != nil {
		return fmt.Errorf("hash administrator password")
	}
	// Shell access is the gate, so a missing account is created rather than
	// refused: an upgraded instance with no administrator has no other way in.
	created, err := store.ResetOrCreateLocalAdmin(ctx, hash, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("could not reset administrator password; check database availability")
	}
	if created {
		fmt.Fprintln(os.Stderr, "requestarr: the local administrator did not exist, so it was created (username admin); sign in with the password below and choose your own")
	}
	if _, err := fmt.Fprintln(os.Stdout, pw); err != nil {
		return fmt.Errorf("password reset succeeded but output failed; run reset-admin-password again")
	}
	return nil
}

// healthcheck makes one bounded GET of /readyz on the local listener. It reads
// only the listener address and never prints the response body, so it needs no
// database, OIDC or secrets configuration and cannot leak any.
func healthcheck(args []string, listenAddr string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	timeout := fs.Duration("timeout", 3*time.Second, "overall time limit for the probe")
	if err := fs.Parse(args); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return fmt.Errorf("invalid listener address")
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/readyz", nil)
	if err != nil {
		return fmt.Errorf("build request")
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return fmt.Errorf("listener unreachable or timed out")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready (status %d)", resp.StatusCode)
	}
	return nil
}
