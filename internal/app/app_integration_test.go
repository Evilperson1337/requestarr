//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
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
	setupToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x61}, 32))
	t.Setenv("REQUESTARR_SETUP_TOKEN", setupToken)
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

	t.Setenv("REQUESTARR_SETUP_TOKEN", "must-not-be-read-after-startup")
	response, err := client.Get("http://" + address + "/api/v1/setup/status")
	if err != nil {
		t.Fatalf("read setup status through application wiring: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK || string(body) != "{\"complete\":false}\n" {
		t.Fatalf("setup status = %d %q, read error %v", response.StatusCode, body, readErr)
	}

	exchangeRequest, err := http.NewRequest(http.MethodPost, "http://"+address+"/api/v1/setup/session", bytes.NewBufferString(`{"token":"`+setupToken+`"}`))
	if err != nil {
		t.Fatalf("construct setup exchange: %v", err)
	}
	exchangeRequest.Header.Set("Content-Type", "application/json")
	exchangeRequest.Header.Set("Origin", "http://localhost:8080")
	exchangeResponse, err := client.Do(exchangeRequest)
	if err != nil {
		t.Fatalf("exchange setup token through application wiring: %v", err)
	}
	_ = exchangeResponse.Body.Close()
	if exchangeResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("setup exchange status = %d", exchangeResponse.StatusCode)
	}
	cookies := exchangeResponse.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "requestarr_setup_dev" || cookies[0].Secure || !cookies[0].HttpOnly || len(cookies[0].Value) != 43 {
		t.Fatalf("setup exchange cookie = %+v", cookies)
	}
	rawSession := cookies[0].Value
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal("open isolated schema for bootstrap assertions")
	}
	var sessions, digestLength, issuedEvents int
	var storedTokenOrSession bool
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM setup_sessions),
		(SELECT octet_length(token_digest) FROM setup_sessions LIMIT 1),
		(SELECT count(*) FROM setup_security_events WHERE event_type='session_issued' AND data='{}'::jsonb),
		EXISTS (SELECT 1 FROM setup_sessions WHERE encode(token_digest,'escape') IN ($1,$2))`, setupToken, rawSession).
		Scan(&sessions, &digestLength, &issuedEvents, &storedTokenOrSession); err != nil {
		pool.Close()
		t.Fatalf("read durable bootstrap effects: %v", err)
	}
	pool.Close()
	if sessions != 1 || digestLength != 32 || issuedEvents != 1 || storedTokenOrSession {
		t.Fatalf("bootstrap persistence sessions=%d digest=%d events=%d raw=%t", sessions, digestLength, issuedEvents, storedTokenOrSession)
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
