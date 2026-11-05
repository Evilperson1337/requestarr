//go:build integration

package postgres

import (
	"testing"

	"github.com/evilperson1337/requestarr/db/migrations"
)

// The wizard's tables and migration stay (D038); the migration must still
// apply and roll back cleanly.
func TestRetiredDraftMigrationRoundTripIntegration(t *testing.T) {
	ctx, s := settingsStore(t)
	seedLegacyDraft(t, ctx, s, "kept")
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) // DDL is isolated and never changes migration history.
	var before string
	if err := tx.QueryRow(ctx, `SELECT (to_jsonb(s)-'draft_revision'-'draft_validations')::text FROM setup_state s WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"000021_setup_drafts.down.sql", "000021_setup_drafts.up.sql"} {
		sql, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var after string
		if err := tx.QueryRow(ctx, `SELECT (to_jsonb(s)-'draft_revision'-'draft_validations')::text FROM setup_state s WHERE singleton`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatalf("%s changed existing state", name)
		}
	}
	var revision int64
	var validations string
	if err := tx.QueryRow(ctx, `SELECT draft_revision,draft_validations::text FROM setup_state WHERE singleton`).Scan(&revision, &validations); err != nil || revision != 0 || validations != "[]" {
		t.Fatalf("migration defaults: revision=%d validations=%s error=%v", revision, validations, err)
	}
	for _, invalid := range []string{`UPDATE setup_state SET draft_revision=-1 WHERE singleton`, `UPDATE setup_state SET draft_validations='{}' WHERE singleton`} {
		if _, err := tx.Exec(ctx, `SAVEPOINT invalid_setup`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, invalid); err == nil {
			t.Fatalf("invalid state accepted: %s", invalid)
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT invalid_setup`); err != nil {
			t.Fatal(err)
		}
	}
}
