//go:build integration

package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"git.example.com/evilperson1337/Requestarr/internal/buildinfo"
	"git.example.com/evilperson1337/Requestarr/internal/config"
	postgresmigrations "git.example.com/evilperson1337/Requestarr/internal/store/postgres/migrations"
)

var appTestSchemaPattern = regexp.MustCompile(`^requestarr_test_[0-9a-f]{32}$`)

func TestRunStartsAfterMigrationAndStopsOnCancellation(t *testing.T) {
	baseURL := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL is required for integration tests")
	}
	databaseURL, cleanup := appTestSchema(t, baseURL)
	defer cleanup()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release test address: %v", err)
	}

	t.Setenv("REQUESTARR_DATABASE_URL", databaseURL)
	t.Setenv("REQUESTARR_HTTP_ADDR", address)
	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test configuration invalid: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), buildinfo.Info{})
	}()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, requestErr := client.Get("http://" + address + "/readyz")
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("application did not become ready")
		}
		time.Sleep(25 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() after cancellation = %v", err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("Run() did not complete bounded shutdown")
	}
}

func TestRunRejectsDirtyMigrationBeforeListening(t *testing.T) {
	baseURL := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL is required for integration tests")
	}
	databaseURL, cleanup := appTestSchema(t, baseURL)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := postgresmigrations.Up(ctx, databaseURL); err != nil {
		t.Fatalf("prepare migrated schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("open isolated application test schema")
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET dirty=TRUE`); err != nil {
		pool.Close()
		t.Fatalf("mark migration history dirty: %v", err)
	}
	pool.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release test address: %v", err)
	}
	t.Setenv("REQUESTARR_DATABASE_URL", databaseURL)
	t.Setenv("REQUESTARR_HTTP_ADDR", address)
	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test configuration invalid: %v", err)
	}

	err = Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), buildinfo.Info{})
	if err == nil || err.Error() != "startup database migration failed" {
		t.Fatalf("Run() error = %v, want sanitized migration startup failure", err)
	}
	probe, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("HTTP listener was opened before migration rejection: %v", err)
	}
	_ = probe.Close()
}

func appTestSchema(t *testing.T, baseURL string) (string, func()) {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	baseConfig, configErr := pgxpool.ParseConfig(baseURL)
	if err != nil || parsed.Fragment != "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || configErr != nil || baseConfig.ConnConfig.Database == "" {
		t.Fatal("REQUESTARR_TEST_DATABASE_URL must be an absolute PostgreSQL URL with a database name")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate application test schema: %v", err)
	}
	schema := "requestarr_test_" + hex.EncodeToString(random)
	if !appTestSchemaPattern.MatchString(schema) {
		t.Fatal("generated application test schema failed safety validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal("open explicit test database URL")
	}
	if _, err := pool.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		pool.Close()
		t.Fatalf("create application test schema: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	targetVerified := false
	isolatedPool, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		pool.Close()
		t.Fatal("open isolated application test schema")
	}
	var currentSchema string
	if err := isolatedPool.QueryRow(ctx, `SELECT current_schema()`).Scan(&currentSchema); err != nil || currentSchema != schema {
		isolatedPool.Close()
		pool.Close()
		t.Fatalf("application test target verification failed: schema=%q err=%v", currentSchema, err)
	}
	targetVerified = true
	isolatedPool.Close()
	return parsed.String(), func() {
		if !targetVerified || !appTestSchemaPattern.MatchString(schema) {
			pool.Close()
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); err != nil {
			t.Errorf("drop application test schema: %v", err)
		}
		pool.Close()
	}
}
