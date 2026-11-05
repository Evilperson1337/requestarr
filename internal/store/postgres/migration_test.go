package postgres

import (
	"os"
	"strings"
	"testing"
)

// golang-migrate's postgres driver executes the complete migration file as one
// Exec by default; PostgreSQL treats that multi-statement query atomically.
// Keep explicit transaction control out of files, which would commit early
// when statements are split by driver configuration.
func TestRequestUseCaseMigrationDoesNotManageTransaction(t *testing.T) {
	data, err := os.ReadFile("../../../db/migrations/000002_request_use_case.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToUpper(string(data))
	for _, statement := range []string{"BEGIN;", "COMMIT;", "ROLLBACK;"} {
		if strings.Contains(sql, statement) {
			t.Errorf("migration contains explicit transaction statement %q", statement)
		}
	}
}

func TestAvailabilityVersionMigrationAddsCycleCounter(t *testing.T) {
	up, err := os.ReadFile("../../../db/migrations/000009_availability_version.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(up), "availability_version BIGINT NOT NULL DEFAULT 0") {
		t.Fatal("availability version migration must add a non-null zero-default counter")
	}
	down, err := os.ReadFile("../../../db/migrations/000009_availability_version.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(down), "DROP COLUMN availability_version") {
		t.Fatal("availability version down migration must remove the column")
	}
}

func TestJellyfinAbsenceMigrationTracksRunsAndDefaultsEmptySnapshotsOff(t *testing.T) {
	up, err := os.ReadFile("../../../db/migrations/000013_jellyfin_absence_reconciliation.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../../db/migrations/000013_jellyfin_absence_reconciliation.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"jellyfin_snapshot_runs", "jellyfin_reconciliation_settings", "allow_empty_snapshots BOOLEAN NOT NULL DEFAULT false", "jellyfin_absence_state", "first_absent_at", "last_absent_at", "absence_count"} {
		if !strings.Contains(string(up), fragment) {
			t.Errorf("migration missing %q", fragment)
		}
	}
	for _, table := range []string{"jellyfin_absence_state", "jellyfin_reconciliation_settings", "jellyfin_snapshot_runs"} {
		if !strings.Contains(string(down), "DROP TABLE "+table) {
			t.Errorf("down migration missing drop for %q", table)
		}
	}
}

func TestAuthMigrationReusesInitialUsersAndRuntimeColumns(t *testing.T) {
	initial, err := os.ReadFile("../../../db/migrations/000001_initial.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	authSQL, err := os.ReadFile("../../../db/migrations/000003_auth.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	runtimeSQL, err := os.ReadFile("../../../db/migrations/000004_auth_runtime.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToUpper(string(authSQL)), "CREATE TABLE USERS") {
		t.Fatal("auth migration duplicates users created by 000001")
	}
	for _, column := range []string{"oidc_subject", "email", "display_name", "role"} {
		if !strings.Contains(string(initial), column) {
			t.Errorf("initial users table lacks %s", column)
		}
	}
	if !strings.Contains(string(authSQL), "preauth_hash") {
		t.Fatal("auth transaction schema lacks pre-auth binding")
	}
	if !strings.Contains(string(runtimeSQL), "oidc_transactions") {
		t.Fatal("runtime migration assumes missing transaction table")
	}
}

func TestAdminBootstrapMigrationExtendsSeededSetupState(t *testing.T) {
	initial, err := os.ReadFile("../../../db/migrations/000001_initial.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../../db/migrations/000007_admin_bootstrap.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../../db/migrations/000007_admin_bootstrap.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	initialSQL, upSQL, downSQL := strings.ToUpper(string(initial)), strings.ToUpper(string(up)), strings.ToUpper(string(down))
	if !strings.Contains(initialSQL, "CREATE TABLE SETUP_STATE") || !strings.Contains(initialSQL, "INSERT INTO SETUP_STATE") {
		t.Fatal("000001 must remain the owner and seeder of setup_state")
	}
	if strings.Contains(upSQL, "CREATE TABLE SETUP_STATE") || strings.Contains(upSQL, "INSERT INTO SETUP_STATE") {
		t.Fatal("000007 must extend setup_state without recreating or reseeding it")
	}
	for _, column := range []string{"target_issuer", "target_subject", "token_hash", "token_expires_at", "claimed_by", "completed_at"} {
		if !strings.Contains(upSQL, "ADD COLUMN "+strings.ToUpper(column)) {
			t.Errorf("000007 does not add setup_state.%s", column)
		}
		if !strings.Contains(downSQL, "DROP COLUMN "+strings.ToUpper(column)) {
			t.Errorf("000007 down migration does not remove setup_state.%s", column)
		}
	}
	if !strings.Contains(upSQL, "CREATE TABLE SETUP_AUDIT_EVENTS") || !strings.Contains(downSQL, "DROP TABLE SETUP_AUDIT_EVENTS") {
		t.Fatal("000007 must create and remove setup_audit_events")
	}
	if strings.Contains(downSQL, "DROP TABLE SETUP_STATE") {
		t.Fatal("000007 down migration must preserve setup_state created and seeded by 000001")
	}
}

func TestAuthProvisioningSQLMatchesMigratedIdentitySchema(t *testing.T) {
	data, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(data)
	for _, required := range []string{"users(id,oidc_subject,email,display_name,role,created_at,updated_at)", "oidc_identities(issuer,subject,user_id,email,display_name)", "ON CONFLICT (oidc_subject)", "RETURNING id::text", "NULLIF($3,'')", "firstSignInRole(stored)", "sha256.Sum256(identity)", "hex.EncodeToString(digest[:])"} {
		if !strings.Contains(sql, required) {
			t.Errorf("auth provisioning SQL missing %q", required)
		}
	}
	for _, invalid := range []string{"users(id,issuer,subject", "ON CONFLICT (issuer,subject) DO NOTHING", `issuer + "\x00" + subject`} {
		if strings.Contains(sql, invalid) {
			t.Errorf("auth provisioning SQL contains stale schema reference %q", invalid)
		}
	}
}

func TestAvailabilityMigrationSeparatesLifecycleAndMakesEventsIdempotent(t *testing.T) {
	data, err := os.ReadFile("../../../db/migrations/000006_availability_core.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(data)
	for _, required := range []string{"DEFAULT 'unknown'", "PRIMARY KEY (catalog_item_id, source_id)", "idempotency_key TEXT NOT NULL UNIQUE"} {
		if !strings.Contains(sql, required) {
			t.Errorf("availability migration missing %q", required)
		}
	}
	if strings.Contains(strings.ToUpper(sql), "UPDATE CATALOG_ITEMS") || strings.Contains(strings.ToLower(sql), "UPDATE REQUESTS") {
		t.Fatal("availability migration must not backfill from request lifecycle")
	}
}

func TestDurableNotificationsMigrationAddsInboxOutboxAndScopedPreferences(t *testing.T) {
	up, err := os.ReadFile("../../../db/migrations/000010_durable_notifications.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(up)
	for _, required := range []string{"event_key", "read_at", "lease_until", "lease_token", "notification_deliveries_outbox_idx", "notification_preferences", "user_id UUID PRIMARY KEY", "ON DELETE CASCADE"} {
		if !strings.Contains(sql, required) {
			t.Errorf("notification migration missing %q", required)
		}
	}
	if strings.Contains(strings.ToUpper(sql), "BEGIN;") || strings.Contains(strings.ToUpper(sql), "COMMIT;") {
		t.Fatal("notification migration must not manage transactions")
	}
	down, err := os.ReadFile("../../../db/migrations/000010_durable_notifications.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(down), "DROP TABLE notification_preferences") {
		t.Fatal("down migration does not remove preferences")
	}
	if !strings.Contains(strings.ToUpper(string(down)), "DATA LOSS") {
		t.Fatal("down migration must document notification data loss")
	}
}
