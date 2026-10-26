package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/evilperson1337/requestarr/internal/app"
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
	if len(os.Args) > 1 && os.Args[1] == "setup-token" {
		if err := issueSetupToken(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "requestarr: %v\n", err)
			os.Exit(1)
		}
		return
	}
	var showVersion, migrateOnly bool
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&migrateOnly, "migrate-only", false, "apply pending database migrations then exit")
	flag.Parse()

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

func issueSetupToken(args []string) error {
	fs := flag.NewFlagSet("setup-token", flag.ContinueOnError)
	issuer := fs.String("issuer", "", "exact configured OIDC issuer")
	subject := fs.String("subject", "", "exact OIDC subject")
	rotate := fs.Bool("rotate", false, "replace an expired outstanding token")
	tokenFile := fs.String("token-file", "", "create token file exclusively with mode 0600")
	stdout := fs.Bool("stdout", false, "explicitly print token to stdout (insecure; may be captured)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !validIssuer(*issuer, config.FromEnv().Environment == "development") || !validSubject(*subject) || fs.NArg() != 0 {
		return fmt.Errorf("setup-token requires a valid --issuer and --subject")
	}
	if *stdout == (*tokenFile != "") {
		return fmt.Errorf("specify exactly one of --token-file PATH or --stdout")
	}
	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database unavailable: %w", err)
	}
	defer store.Close()
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	if err := store.IssueAdminToken(ctx, *issuer, *subject, token, time.Now().Add(30*time.Minute), *rotate); err != nil {
		return fmt.Errorf("could not issue setup token")
	}
	if *stdout {
		_, err = fmt.Fprintln(os.Stdout, token)
		return err
	}
	return writeTokenFile(*tokenFile, token)
}

func validIssuer(raw string, development bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.TrimSpace(raw) != raw || strings.Contains(u.Path, "/../") || strings.HasSuffix(u.Path, "/..") || strings.Contains(u.Path, "/./") || strings.HasSuffix(u.Path, "/.") {
		return false
	}
	if u.Scheme != "https" && !(development && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")) {
		return false
	}
	return u.String() == raw
}

func validSubject(subject string) bool {
	if subject == "" || len(subject) > 1024 || strings.TrimSpace(subject) != subject {
		return false
	}
	for _, r := range subject {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func writeTokenFile(path, token string) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create token file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close token file: %w", closeErr)
		}
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if _, err = fmt.Fprintln(f, token); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("sync token file: %w", err)
	}
	return nil
}
