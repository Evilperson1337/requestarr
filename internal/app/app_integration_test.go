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
	"git.example.com/evilperson1337/Requestarr/internal/setup"
	"git.example.com/evilperson1337/Requestarr/internal/store/postgres"
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

	client := &http.Client{Timeout: 3 * time.Second}
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
	generalRequest, err := http.NewRequest(http.MethodPut, "http://"+address+"/api/v1/setup/general", bytes.NewBufferString(`{"instance_name":" Requestarr ","public_url":"http://localhost:8080/","timezone":"UTC"}`))
	if err != nil {
		t.Fatalf("construct General request: %v", err)
	}
	generalRequest.Header.Set("Content-Type", "application/json")
	generalRequest.Header.Set("Origin", "http://localhost:8080")
	generalRequest.Header.Set("X-Requestarr-CSRF", "1")
	generalRequest.AddCookie(cookies[0])
	generalResponse, err := client.Do(generalRequest)
	if err != nil {
		t.Fatalf("save General through application wiring: %v", err)
	}
	generalBody, readErr := io.ReadAll(generalResponse.Body)
	_ = generalResponse.Body.Close()
	if readErr != nil || generalResponse.StatusCode != http.StatusOK || string(generalBody) != `{"current_step":"authentication","general":{"instance_name":"Requestarr","public_url":"http://localhost:8080","timezone":"UTC"}}`+"\n" {
		t.Fatalf("General response = %d %q, read error %v", generalResponse.StatusCode, generalBody, readErr)
	}
	stateRequest, err := http.NewRequest(http.MethodGet, "http://"+address+"/api/v1/setup/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	stateRequest.AddCookie(cookies[0])
	stateResponse, err := client.Do(stateRequest)
	if err != nil {
		t.Fatalf("resume setup state through application wiring: %v", err)
	}
	stateBody, readErr := io.ReadAll(stateResponse.Body)
	_ = stateResponse.Body.Close()
	if readErr != nil || stateResponse.StatusCode != http.StatusOK || !bytes.Equal(stateBody, generalBody) {
		t.Fatalf("resumed state = %d %q, read error %v", stateResponse.StatusCode, stateBody, readErr)
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal("open isolated schema for bootstrap assertions")
	}
	if _, err := pool.Exec(context.Background(), `UPDATE setup_sessions
		SET expires_at=clock_timestamp()+interval '300 milliseconds'`); err != nil {
		pool.Close()
		t.Fatalf("shorten setup session lifetime: %v", err)
	}
	lock, err := pool.Begin(context.Background())
	if err != nil {
		pool.Close()
		t.Fatalf("begin setup-state lock: %v", err)
	}
	if _, err := lock.Exec(context.Background(), `SELECT 1 FROM setup_state WHERE singleton=TRUE FOR UPDATE`); err != nil {
		_ = lock.Rollback(context.Background())
		pool.Close()
		t.Fatalf("acquire setup-state lock: %v", err)
	}
	expiredResponse := make(chan *http.Response, 1)
	expiredError := make(chan error, 1)
	go func() {
		request, err := http.NewRequest(http.MethodPut, "http://"+address+"/api/v1/setup/general", bytes.NewBufferString(`{"instance_name":"must-not-save","public_url":"http://localhost:8080","timezone":"UTC"}`))
		if err == nil {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "http://localhost:8080")
			request.Header.Set("X-Requestarr-CSRF", "1")
			request.AddCookie(cookies[0])
			expiredResponseValue, requestErr := client.Do(request)
			expiredResponse <- expiredResponseValue
			expiredError <- requestErr
			return
		}
		expiredResponse <- nil
		expiredError <- err
	}()
	select {
	case response := <-expiredResponse:
		if response != nil {
			_ = response.Body.Close()
		}
		_ = lock.Rollback(context.Background())
		pool.Close()
		t.Fatal("General save did not wait for setup-state lock")
	case <-time.After(500 * time.Millisecond):
	}
	if err := lock.Commit(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("release setup-state lock: %v", err)
	}
	expired := <-expiredResponse
	if err := <-expiredError; err != nil {
		pool.Close()
		t.Fatalf("expired blocked General request: %v", err)
	}
	expiredBody, readErr := io.ReadAll(expired.Body)
	_ = expired.Body.Close()
	if readErr != nil || expired.StatusCode != http.StatusUnauthorized || !bytes.Contains(expiredBody, []byte(`"code":"setup_session_required"`)) {
		pool.Close()
		t.Fatalf("expired blocked General response=%d body=%q read=%v", expired.StatusCode, expiredBody, readErr)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE setup_sessions SET expires_at=clock_timestamp()+interval '15 minutes'`); err != nil {
		pool.Close()
		t.Fatalf("reactivate setup session for completion: %v", err)
	}
	completionPool, err := postgres.Open(context.Background(), databaseURL)
	if err != nil {
		pool.Close()
		t.Fatalf("open completion repository pool: %v", err)
	}
	bootstrapRepository := postgres.NewSetupBootstrapRepository(completionPool)
	var revocationID, completionID setup.PersistenceID
	for i := range revocationID {
		revocationID[i] = byte(i + 1)
		completionID[i] = byte(i + 33)
	}
	if _, err := bootstrapRepository.Complete(context.Background(), setup.CompletionRequest{
		RevocationEventID: revocationID, CompletionEventID: completionID,
	}); err != nil {
		completionPool.Close()
		pool.Close()
		t.Fatalf("complete setup through repository: %v", err)
	}
	completionPool.Close()
	completedRequest, err := http.NewRequest(http.MethodPut, "http://"+address+"/api/v1/setup/general", bytes.NewBufferString(`{"instance_name":"must-not-save","public_url":"http://localhost:8080","timezone":"UTC"}`))
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	completedRequest.Header.Set("Content-Type", "application/json")
	completedRequest.Header.Set("Origin", "http://localhost:8080")
	completedRequest.Header.Set("X-Requestarr-CSRF", "1")
	completedRequest.AddCookie(cookies[0])
	completedResponse, err := client.Do(completedRequest)
	if err != nil {
		pool.Close()
		t.Fatalf("completed General request: %v", err)
	}
	completedBody, readErr := io.ReadAll(completedResponse.Body)
	_ = completedResponse.Body.Close()
	if readErr != nil || completedResponse.StatusCode != http.StatusConflict || !bytes.Contains(completedBody, []byte(`"code":"setup_completed"`)) {
		pool.Close()
		t.Fatalf("completed General response=%d body=%q read=%v", completedResponse.StatusCode, completedBody, readErr)
	}
	completedStateRequest, err := http.NewRequest(http.MethodGet, "http://"+address+"/api/v1/setup/state", nil)
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	completedStateRequest.AddCookie(cookies[0])
	completedStateResponse, err := client.Do(completedStateRequest)
	if err != nil {
		pool.Close()
		t.Fatalf("completed setup state request: %v", err)
	}
	_ = completedStateResponse.Body.Close()
	if completedStateResponse.StatusCode != http.StatusUnauthorized {
		pool.Close()
		t.Fatalf("completed setup state status=%d, want 401", completedStateResponse.StatusCode)
	}
	var sessions, digestLength, issuedEvents int
	var storedTokenOrSession bool
	var workflowEvents int
	var unchangedDraft bool
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM setup_sessions),
		(SELECT octet_length(token_digest) FROM setup_sessions LIMIT 1),
		(SELECT count(*) FROM setup_security_events WHERE event_type='session_issued' AND data='{}'::jsonb),
		EXISTS (SELECT 1 FROM setup_sessions WHERE encode(token_digest,'escape') IN ($1,$2)),
		(SELECT count(*) FROM setup_workflow_events WHERE event_type='general_saved'),
		(SELECT draft->'general'->>'instance_name' = 'Requestarr' FROM setup_state WHERE singleton=TRUE)`, setupToken, rawSession).
		Scan(&sessions, &digestLength, &issuedEvents, &storedTokenOrSession, &workflowEvents, &unchangedDraft); err != nil {
		pool.Close()
		t.Fatalf("read durable bootstrap effects: %v", err)
	}
	pool.Close()
	if sessions != 1 || digestLength != 32 || issuedEvents != 1 || storedTokenOrSession || workflowEvents != 1 || !unchangedDraft {
		t.Fatalf("bootstrap persistence sessions=%d digest=%d security_events=%d raw=%t workflow_events=%d unchanged=%t", sessions, digestLength, issuedEvents, storedTokenOrSession, workflowEvents, unchangedDraft)
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
