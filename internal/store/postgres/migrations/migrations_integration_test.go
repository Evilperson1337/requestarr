//go:build integration

package migrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"net/url"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	embedded "git.example.com/evilperson1337/Requestarr/db/migrations"
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
	if version != 3 || dirty {
		t.Fatalf("migration state = version %d dirty %t, want version 3 clean", version, dirty)
	}

	for _, table := range []string{"users", "catalog_items", "requests", "request_subscriptions", "request_events", "library_sources", "inventory_items", "notification_deliveries", "setup_state", "setup_sessions", "setup_exchange_rate_limits", "setup_security_events", "setup_workflow_events"} {
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

func TestSetupWorkflowAuditMigrationFromExactV2AndRollback(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, name := range []string{"000001_initial.up.sql", "000002_setup_bootstrap_sessions.up.sql"} {
		migration, err := fs.ReadFile(embedded.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES (2, false)`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+".setup_workflow_events").Scan(&exists); err != nil || !exists {
		t.Fatalf("workflow audit table exists=%t err=%v", exists, err)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_workflow_events (event_type,created_at) VALUES ('secret_saved',clock_timestamp())`)
	down, err := fs.ReadFile(embedded.Files, "000003_setup_workflow_events.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+".setup_workflow_events").Scan(&exists); err != nil || exists {
		t.Fatalf("rolled back workflow audit table exists=%t err=%v", exists, err)
	}
}

func TestIntegrationDatabaseIsPostgreSQL17(t *testing.T) {
	_, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var major int
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::integer / 10000`).Scan(&major); err != nil {
		t.Fatalf("read PostgreSQL version: %v", err)
	}
	if major != 17 {
		t.Fatalf("PostgreSQL major version = %d, want 17", major)
	}
}

func TestSetupBootstrapMigrationFromExactV1AndRollback(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	v1, err := fs.ReadFile(embedded.Files, "000001_initial.up.sql")
	if err != nil {
		t.Fatalf("read v1 migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(v1)); err != nil {
		t.Fatalf("apply exact v1 schema: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES (1, false)`); err != nil {
		t.Fatalf("seed exact v1 history: %v", err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("migrate exact v1 to v2: %v", err)
	}
	assertSetupBootstrapSchema(t, ctx, pool, schema)

	down, err := fs.ReadFile(embedded.Files, "000002_setup_bootstrap_sessions.down.sql")
	if err != nil {
		t.Fatalf("read v2 down migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("apply local/test v2 rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version = 1, dirty = false`); err != nil {
		t.Fatalf("reset local/test migration history: %v", err)
	}
	for _, table := range []string{"setup_security_events", "setup_exchange_rate_limits", "setup_sessions"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil || exists {
			t.Fatalf("rolled-back table %s exists=%t err=%v", table, exists, err)
		}
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v2 after rollback: %v", err)
	}
	assertSetupBootstrapSchema(t, ctx, pool, schema)
}

func TestSetupBootstrapMigrationConstraints(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	validDigest := make([]byte, 32)
	validID := "00000000-0000-4000-8000-000000000001"
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_sessions VALUES ($1,$2,$3,$4,NULL)`, validID, make([]byte, 31), now, now.Add(time.Minute))
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_sessions VALUES ($1,$2,$3,$4,NULL)`, validID, validDigest, now, now)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_sessions VALUES ($1,$2,$3,$4,$5)`, validID, validDigest, now, now.Add(time.Minute), now.Add(-time.Second))
	if _, err := pool.Exec(ctx, `INSERT INTO setup_sessions VALUES ($1,$2,$3,$4,NULL)`, validID, validDigest, now, now.Add(time.Minute)); err != nil {
		t.Fatalf("insert valid session: %v", err)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_sessions VALUES ($1,$2,$3,$4,NULL)`, "00000000-0000-4000-8000-000000000002", validDigest, now, now.Add(time.Minute))

	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_exchange_rate_limits VALUES ($1,$2,$3,$4,$5)`, validDigest, "other", now, 1, now)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_exchange_rate_limits VALUES ($1,$2,$3,$4,$5)`, make([]byte, 31), "peer", now, 1, now)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_exchange_rate_limits VALUES ($1,$2,$3,$4,$5)`, validDigest, "peer", now, 0, now)
	if _, err := pool.Exec(ctx, `INSERT INTO setup_exchange_rate_limits VALUES ($1,'peer',$2,1,$2)`, validDigest, now); err != nil {
		t.Fatalf("insert valid rate row: %v", err)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_exchange_rate_limits VALUES ($1,'peer',$2,1,$2)`, validDigest, now)
	if _, err := pool.Exec(ctx, `INSERT INTO setup_exchange_rate_limits VALUES ($1,'peer',$2,1,$2)`, validDigest, now.Add(15*time.Minute)); err != nil {
		t.Fatalf("same rate key in later window rejected: %v", err)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_security_events (id,event_type,created_at) VALUES ($1,'not_allowed',$2)`, "00000000-0000-4000-8000-000000000003", now)
	if _, err := pool.Exec(ctx, `INSERT INTO setup_security_events (id,event_type,session_id,created_at)
		VALUES ($1,'session_issued',$2,$3)`, "00000000-0000-4000-8000-000000000003", validID, now); err != nil {
		t.Fatalf("insert event referencing session: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM setup_sessions WHERE id=$1`, validID); err != nil {
		t.Fatalf("delete referenced session: %v", err)
	}
	var eventSessionID *string
	if err := pool.QueryRow(ctx, `SELECT session_id::text FROM setup_security_events WHERE id=$1`, "00000000-0000-4000-8000-000000000003").Scan(&eventSessionID); err != nil || eventSessionID != nil {
		t.Fatalf("event session after delete = %v, %v; want NULL", eventSessionID, err)
	}
}

func assertSetupBootstrapSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string) {
	t.Helper()
	for _, table := range []string{"setup_sessions", "setup_exchange_rate_limits", "setup_security_events"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil || !exists {
			t.Fatalf("expected table %s: exists=%t err=%v", table, exists, err)
		}
	}
	for _, index := range []string{"setup_sessions_token_digest_key", "setup_sessions_expires_at_idx", "setup_sessions_unrevoked_expires_at_idx", "setup_exchange_rate_limits_pkey", "setup_exchange_rate_limits_window_start_idx", "setup_security_events_created_at_idx"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+index).Scan(&exists); err != nil || !exists {
			t.Fatalf("expected index %s: exists=%t err=%v", index, exists, err)
		}
	}
	var foreignKeys int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_constraint
		WHERE conrelid = $1::regclass AND contype = 'f' AND confrelid = $2::regclass`,
		schema+".setup_security_events", schema+".setup_sessions").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("setup security event foreign keys=%d err=%v", foreignKeys, err)
	}
}

func assertConstraintFailure(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, query, args...); err == nil {
		t.Fatalf("query unexpectedly satisfied constraint: %s", query)
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
		{name: "ahead", version: 4, wantError: "migration history version is newer than this binary"},
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
