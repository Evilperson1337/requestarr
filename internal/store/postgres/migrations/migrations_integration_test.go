//go:build integration

package migrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testSchemaPattern = regexp.MustCompile(`^requestarr_test_[0-9a-f]{32}$`)

func TestEmbeddedMigrationsCleanAndIdempotent(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("first Up() failed: %v", err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("second Up() failed: %v", err)
	}

	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read migration history: %v", err)
	}
	if version != 1 || dirty {
		t.Fatalf("migration state = version %d dirty %t, want version 1 clean", version, dirty)
	}

	for _, table := range []string{"users", "catalog_items", "requests", "request_subscriptions", "request_events", "library_sources", "inventory_items", "notification_deliveries", "setup_state"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil || !exists {
			t.Fatalf("expected table %s.%s to exist: exists=%t err=%v", schema, table, exists, err)
		}
	}

	var setupRows int
	var singleton, complete bool
	var currentStep string
	if err := pool.QueryRow(ctx, `SELECT count(*), bool_and(singleton), bool_or(complete), min(current_step) FROM setup_state`).Scan(&setupRows, &singleton, &complete, &currentStep); err != nil {
		t.Fatalf("read setup singleton: %v", err)
	}
	if setupRows != 1 || !singleton || complete || currentStep != "welcome" {
		t.Fatalf("setup state = rows %d singleton %t complete %t step %q, want one incomplete welcome singleton", setupRows, singleton, complete, currentStep)
	}
}

func TestIntegrationDatabaseURLDoesNotFallBackToRuntimeConfiguration(t *testing.T) {
	t.Setenv("REQUESTARR_TEST_DATABASE_URL", "")
	t.Setenv("REQUESTARR_DATABASE_URL", "postgres://production-user:production-secret@production.example/requestarr")
	if databaseURL, configured := integrationDatabaseURL(); configured || databaseURL != "" {
		t.Fatalf("integrationDatabaseURL() = %q, %t; want no configured test database", databaseURL, configured)
	}
}

func TestEmbeddedMigrationsRejectDirtyAndAheadHistory(t *testing.T) {
	tests := []struct {
		name      string
		version   int
		dirty     bool
		wantError string
	}{
		{name: "dirty", version: 1, dirty: true, wantError: "migration history is dirty"},
		{name: "ahead", version: 2, wantError: "migration history version is newer than this binary"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			databaseURL, _, pool := isolatedDatabase(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := Up(ctx, databaseURL); err != nil {
				t.Fatalf("initial Up() failed: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=$1, dirty=$2`, tt.version, tt.dirty); err != nil {
				t.Fatalf("modify migration history: %v", err)
			}
			if err := Up(ctx, databaseURL); err == nil || err.Error() != tt.wantError {
				t.Fatalf("Up() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestConcurrentMigratorsShareAdvisoryLock(t *testing.T) {
	databaseURL, _, _ := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := make(chan struct{})
	errs := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errs <- Up(ctx, databaseURL)
		}()
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Up() failed: %v", err)
		}
	}
}

func TestMigrationSchemasAreIsolated(t *testing.T) {
	firstURL, firstSchema, firstPool := isolatedDatabase(t)
	secondURL, secondSchema, secondPool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, firstURL); err != nil {
		t.Fatalf("first schema migration failed: %v", err)
	}
	var secondHistoryBeforeMigration bool
	if err := firstPool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, secondSchema+".schema_migrations").Scan(&secondHistoryBeforeMigration); err != nil {
		t.Fatalf("check untouched second schema: %v", err)
	}
	if secondHistoryBeforeMigration {
		t.Fatalf("migration in %s unexpectedly created history in %s", firstSchema, secondSchema)
	}
	if err := Up(ctx, secondURL); err != nil {
		t.Fatalf("second schema migration failed: %v", err)
	}

	for _, check := range []struct {
		pool  *pgxpool.Pool
		own   string
		other string
	}{
		{pool: firstPool, own: firstSchema, other: secondSchema},
		{pool: secondPool, own: secondSchema, other: firstSchema},
	} {
		var ownHistory, otherHistory bool
		if err := check.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL, to_regclass($2) IS NOT NULL`, check.own+".schema_migrations", check.other+".schema_migrations").Scan(&ownHistory, &otherHistory); err != nil {
			t.Fatalf("check isolated history: %v", err)
		}
		if !ownHistory || !otherHistory {
			t.Fatalf("expected independent histories in %s and %s", check.own, check.other)
		}
	}
}

func isolatedDatabase(t *testing.T) (string, string, *pgxpool.Pool) {
	t.Helper()
	baseURL, configured := integrationDatabaseURL()
	if !configured {
		t.Skip("REQUESTARR_TEST_DATABASE_URL is required for integration tests")
	}
	parsed, err := url.Parse(baseURL)
	baseConfig, configErr := pgxpool.ParseConfig(baseURL)
	if err != nil || parsed.Fragment != "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || configErr != nil || baseConfig.ConnConfig.Database == "" {
		t.Fatal("REQUESTARR_TEST_DATABASE_URL must be an absolute PostgreSQL URL with a database name")
	}

	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate test schema name: %v", err)
	}
	schema := "requestarr_test_" + hex.EncodeToString(random)
	if !testSchemaPattern.MatchString(schema) {
		t.Fatal("generated schema failed safety validation")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal("open explicit test database URL")
	}
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		adminPool.Close()
		t.Fatalf("create isolated test schema: %v", err)
	}
	targetVerified := false
	t.Cleanup(func() {
		if !targetVerified || !testSchemaPattern.MatchString(schema) {
			adminPool.Close()
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); err != nil {
			t.Errorf("drop isolated test schema: %v", err)
		}
		adminPool.Close()
	})

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	isolatedURL := parsed.String()
	pool, err := pgxpool.New(ctx, isolatedURL)
	if err != nil {
		t.Fatal("open isolated test schema")
	}
	t.Cleanup(pool.Close)
	var currentSchema string
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&currentSchema); err != nil || currentSchema != schema {
		t.Fatalf("isolated target verification failed: schema=%q err=%v", currentSchema, err)
	}
	targetVerified = true

	return isolatedURL, schema, pool
}

func integrationDatabaseURL() (string, bool) {
	databaseURL := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	return databaseURL, databaseURL != ""
}
