package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/buildinfo"
	"git.example.com/evilperson1337/Requestarr/internal/config"
)

type readinessStub struct{ err error }

func (s readinessStub) Ready(context.Context) error { return s.err }

func TestReadinessRequiresMigrationsAndDatabase(t *testing.T) {
	checker := &readinessChecker{database: readinessStub{}}
	if err := checker.Ready(context.Background()); err == nil {
		t.Fatal("Ready() succeeded before migrations completed")
	}

	checker.migrationsComplete.Store(true)
	if err := checker.Ready(context.Background()); err != nil {
		t.Fatalf("Ready() after migrations = %v", err)
	}

	checker.database = readinessStub{err: errors.New("unavailable")}
	if err := checker.Ready(context.Background()); err == nil {
		t.Fatal("Ready() succeeded while database was unavailable")
	}
}

func TestRunDatabaseStartupFailureIsBoundedSanitizedAndDoesNotListen(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release test address: %v", err)
	}

	secret := "startup-secret"
	t.Setenv("REQUESTARR_DATABASE_URL", "postgres://requestarr:"+secret+"@203.0.113.1:1/requestarr?sslmode=disable")
	t.Setenv("REQUESTARR_HTTP_ADDR", address)
	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test configuration invalid: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), buildinfo.Info{})
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Run() database failure took %v, want bounded completion", elapsed)
	}
	if err == nil || err.Error() != "startup database connection failed" {
		t.Fatalf("Run() error = %v, want sanitized database startup failure", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "postgres://") {
		t.Fatalf("Run() error exposed database configuration: %v", err)
	}
	probe, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("HTTP listener was opened after database startup failure: %v", err)
	}
	_ = probe.Close()
}
