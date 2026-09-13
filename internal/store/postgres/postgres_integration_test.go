//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"
)

// testDatabaseURL returns the database used only by these integration tests.
// It intentionally reads a dedicated env var, never the application's own
// REQUESTARR_DATABASE_URL, so a test run can never touch a developer's real
// database.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	return url
}

func TestOpenAppliesMigrations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	rows, err := store.pool.Query(ctx, `select table_name from information_schema.tables where table_schema = 'public' order by table_name`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	for _, want := range []string{"users", "catalog_items", "requests", "request_subscriptions", "request_events", "library_sources", "inventory_items", "notification_deliveries", "setup_state"} {
		found := false
		for _, got := range tables {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected table %q to exist after migration, tables = %v", want, tables)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := testDatabaseURL(t)

	first, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	first.Close()

	second, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("second Open() (rerun) error = %v", err)
	}
	defer second.Close()

	if err := second.Ready(ctx); err != nil {
		t.Fatalf("Ready() after rerun = %v", err)
	}
}
