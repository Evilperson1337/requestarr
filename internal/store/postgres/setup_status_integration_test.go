//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"git.example.com/evilperson1337/Requestarr/internal/setup"
	postgresmigrations "git.example.com/evilperson1337/Requestarr/internal/store/postgres/migrations"
)

var setupTestSchemaPattern = regexp.MustCompile(`^requestarr_test_[0-9a-f]{32}$`)

func TestSetupStatusRepositoryReadsAndValidatesPersistedState(t *testing.T) {
	databaseURL, rawPool := isolatedSetupDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := postgresmigrations.Up(ctx, databaseURL); err != nil {
		t.Fatalf("migrate setup repository schema: %v", err)
	}
	repository := NewSetupStatusRepository(&Pool{pool: rawPool})

	state, err := repository.ReadState(ctx)
	if err != nil || state.Complete || state.Current != setup.StepWelcome {
		t.Fatalf("initial ReadState() = %+v, %v", state, err)
	}
	if _, err := rawPool.Exec(ctx, `UPDATE setup_state SET complete=TRUE, current_step='finish' WHERE singleton=TRUE`); err != nil {
		t.Fatalf("update setup state: %v", err)
	}
	state, err = repository.ReadState(ctx)
	if err != nil || !state.Complete || state.Current != setup.StepFinish {
		t.Fatalf("complete ReadState() = %+v, %v", state, err)
	}

	if _, err := rawPool.Exec(ctx, `UPDATE setup_state SET current_step='malformed' WHERE singleton=TRUE`); err != nil {
		t.Fatalf("malform setup state: %v", err)
	}
	if _, err := repository.ReadState(ctx); !errors.Is(err, setup.ErrStateInvalid) {
		t.Fatalf("malformed-step ReadState() error = %v, want invalid state", err)
	}

	if _, err := rawPool.Exec(ctx, `DELETE FROM setup_state`); err != nil {
		t.Fatalf("delete setup singleton: %v", err)
	}
	if _, err := repository.ReadState(ctx); !errors.Is(err, setup.ErrStateMissing) {
		t.Fatalf("missing ReadState() error = %v", err)
	}
}

func TestSetupStatusRepositoryRejectsImpossibleDuplicateAndMalformedTypes(t *testing.T) {
	databaseURL, rawPool := isolatedSetupDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := postgresmigrations.Up(ctx, databaseURL); err != nil {
		t.Fatalf("migrate setup repository schema: %v", err)
	}
	repository := NewSetupStatusRepository(&Pool{pool: rawPool})

	// Simulate impossible corruption by replacing the constrained table only in
	// this isolated schema. Production migration history remains untouched.
	if _, err := rawPool.Exec(ctx, `DROP TABLE setup_state; CREATE TABLE setup_state(singleton BOOLEAN, complete TEXT, current_step TEXT); INSERT INTO setup_state VALUES(TRUE, 'false', 'welcome')`); err != nil {
		t.Fatalf("prepare malformed setup state: %v", err)
	}
	if _, err := repository.ReadState(ctx); !errors.Is(err, setup.ErrStateInvalid) {
		t.Fatalf("malformed ReadState() error = %v", err)
	}
	if _, err := rawPool.Exec(ctx, `TRUNCATE setup_state; ALTER TABLE setup_state ALTER COLUMN complete TYPE BOOLEAN USING complete::BOOLEAN; INSERT INTO setup_state VALUES(TRUE, FALSE, 'welcome'), (TRUE, TRUE, 'finish')`); err != nil {
		t.Fatalf("prepare duplicate setup state: %v", err)
	}
	if _, err := repository.ReadState(ctx); !errors.Is(err, setup.ErrStateDuplicate) {
		t.Fatalf("duplicate ReadState() error = %v", err)
	}
}

func TestSetupStatusRepositoryRejectsNullPersistedValues(t *testing.T) {
	databaseURL, rawPool := isolatedSetupDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := postgresmigrations.Up(ctx, databaseURL); err != nil {
		t.Fatalf("migrate setup repository schema: %v", err)
	}
	repository := NewSetupStatusRepository(&Pool{pool: rawPool})

	if _, err := rawPool.Exec(ctx, `ALTER TABLE setup_state ALTER COLUMN complete DROP NOT NULL, ALTER COLUMN current_step DROP NOT NULL`); err != nil {
		t.Fatalf("allow impossible NULL setup state: %v", err)
	}
	for _, column := range []string{"complete", "current_step"} {
		t.Run(column, func(t *testing.T) {
			if _, err := rawPool.Exec(ctx, `UPDATE setup_state SET complete=FALSE, current_step='welcome'`); err != nil {
				t.Fatalf("reset setup state: %v", err)
			}
			if _, err := rawPool.Exec(ctx, `UPDATE setup_state SET `+pgx.Identifier{column}.Sanitize()+`=NULL`); err != nil {
				t.Fatalf("set %s NULL: %v", column, err)
			}
			if _, err := repository.ReadState(ctx); !errors.Is(err, setup.ErrStateInvalid) {
				t.Fatalf("ReadState() error = %v, want invalid state", err)
			}
		})
	}
}

func TestSetupStatusRepositoryCancellationAndDatabaseFailureAreSafe(t *testing.T) {
	databaseURL, rawPool := isolatedSetupDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := postgresmigrations.Up(ctx, databaseURL); err != nil {
		t.Fatalf("migrate setup repository schema: %v", err)
	}
	repository := NewSetupStatusRepository(&Pool{pool: rawPool})

	canceled, cancelRead := context.WithCancel(context.Background())
	cancelRead()
	if _, err := repository.ReadState(canceled); !errors.Is(err, setup.ErrStatusCanceled) {
		t.Fatalf("canceled ReadState() error = %v", err)
	}
	rawPool.Close()
	if _, err := repository.ReadState(context.Background()); !errors.Is(err, setup.ErrStatusUnavailable) {
		t.Fatalf("closed-pool ReadState() error = %v", err)
	}
}

func TestSetupIntegrationURLDoesNotFallBackToRuntimeURL(t *testing.T) {
	t.Setenv("REQUESTARR_TEST_DATABASE_URL", "")
	t.Setenv("REQUESTARR_DATABASE_URL", "postgres://production:secret@production.example/requestarr")
	if got := os.Getenv("REQUESTARR_TEST_DATABASE_URL"); got != "" {
		t.Fatalf("test URL = %q, want empty", got)
	}
}

func isolatedSetupDatabase(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	baseURL := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL is required for integration tests")
	}
	parsed, err := url.Parse(baseURL)
	baseConfig, configErr := pgxpool.ParseConfig(baseURL)
	if err != nil || parsed.Fragment != "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || configErr != nil || baseConfig.ConnConfig.Database == "" {
		t.Fatal("REQUESTARR_TEST_DATABASE_URL must be an absolute PostgreSQL URL with a database name")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate setup test schema: %v", err)
	}
	schema := "requestarr_test_" + hex.EncodeToString(random)
	if !setupTestSchemaPattern.MatchString(schema) {
		t.Fatal("generated setup test schema failed safety validation")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal("open explicit test database URL")
	}
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		adminPool.Close()
		t.Fatalf("create setup test schema: %v", err)
	}
	targetVerified := false
	t.Cleanup(func() {
		if !targetVerified || !setupTestSchemaPattern.MatchString(schema) {
			adminPool.Close()
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); err != nil {
			t.Errorf("drop setup test schema: %v", err)
		}
		adminPool.Close()
	})

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	isolatedURL := parsed.String()
	pool, err := pgxpool.New(ctx, isolatedURL)
	if err != nil {
		t.Fatal("open isolated setup test schema")
	}
	t.Cleanup(pool.Close)
	var currentSchema string
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&currentSchema); err != nil || currentSchema != schema {
		t.Fatalf("setup test target verification failed: schema=%q err=%v", currentSchema, err)
	}
	targetVerified = true
	return isolatedURL, pool
}
