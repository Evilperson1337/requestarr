//go:build integration

package migrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
	if version != 27 || dirty {
		t.Fatalf("migration state = version %d dirty %t, want version 27 clean", version, dirty)
	}

	for _, table := range []string{"users", "catalog_items", "requests", "request_subscriptions", "request_events", "library_sources", "inventory_items", "notification_deliveries", "setup_state", "setup_sessions", "setup_exchange_rate_limits", "setup_security_events", "setup_workflow_events", "encryption_key_registry", "encrypted_secrets", "setup_authentication_validation_evidence", "setup_authentication_validation_rate_limits", "reconciliation_match_evidence", "reconciliation_match_events", "auth_runtime_config", "auth_runtime_activation", "auth_login_transactions", "auth_sessions", "auth_audit_events", "inventory_authoritative_cycles", "inventory_authoritative_cycle_events", "inventory_source_status", "inventory_source_status_transitions", "durable_jobs", "durable_job_events", "inventory_source_configurations", "inventory_source_selected_libraries", "inventory_source_configuration_audit", "inventory_source_configuration_audit_selected_libraries", "inventory_source_configuration_revisions", "inventory_source_configuration_revision_selected_libraries", "inventory_source_configuration_credential_revisions", "inventory_manual_sync_intents", "inventory_manual_sync_request_attempts"} {
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

func TestRecoverDirtyV20ReplaysActualFailedMigration(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000019_source_configuration_projection_integrity.up.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES (19, false)`); err != nil {
		t.Fatalf("seed v19 migration history: %v", err)
	}

	// The unmodified v20 script fails when it tries to CREATE an integrity
	// function that v19 already provides. Its transaction rolls all v20 objects
	// back, while golang-migrate truthfully records dirty v20.
	if err := Up(ctx, databaseURL); err == nil {
		t.Fatal("unmodified v20 unexpectedly succeeded")
	}
	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if version != 20 || !dirty {
		t.Fatalf("failed migration history = %d dirty=%t, want dirty v20", version, dirty)
	}
	if err := RecoverDirtyV20(ctx, databaseURL); err != nil {
		t.Fatalf("recover actual failed v20: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if version != 24 || dirty {
		t.Fatalf("recovered migration history = %d dirty=%t, want clean v24", version, dirty)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('inventory_source_configuration_credential_revisions') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		t.Fatalf("credential evidence table missing after recovery: exists=%t err=%v", exists, err)
	}
}

func TestRecoverDirtyV20RefusesUnexpectedPhysicalState(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000019_source_configuration_projection_integrity.up.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES (20, true); CREATE TABLE inventory_source_configuration_credential_revisions (source_id UUID)`); err != nil {
		t.Fatal(err)
	}
	if err := RecoverDirtyV20(ctx, databaseURL); !errors.Is(err, ErrDirtyV20RecoveryNotEligible) && err.Error() != "migration recovery physical certification failed" {
		t.Fatalf("recovery error = %v, want safe refusal", err)
	}
	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if version != 20 || !dirty {
		t.Fatalf("refusal changed migration history to %d dirty=%t", version, dirty)
	}
}

func TestSourceConfigurationWriteLockHandlesAllProjectionMutationOperations(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("apply migration chain: %v", err)
	}

	// Roll back instead of committing deliberately incomplete projections. This
	// directly exercises the BEFORE triggers' INSERT, UPDATE, and DELETE paths
	// without bypassing the deferred integrity constraints they protect.
	if _, err := pool.Exec(ctx, `BEGIN;
		INSERT INTO library_sources(id,kind,name,enabled,authoritative_media_types,created_at,updated_at)
		VALUES ('00000000-0000-4000-8000-000000000601','plex','Lock test',true,'["movie"]',clock_timestamp(),clock_timestamp());
		INSERT INTO inventory_source_configurations(source_id,endpoint_url,revision,updated_at)
		VALUES ('00000000-0000-4000-8000-000000000601','https://plex.example.test',1,clock_timestamp());
		INSERT INTO inventory_source_selected_libraries(source_id,remote_library_id)
		VALUES ('00000000-0000-4000-8000-000000000601','one');
		UPDATE library_sources SET name='Updated lock test' WHERE id='00000000-0000-4000-8000-000000000601';
		UPDATE inventory_source_configurations SET endpoint_url='https://updated.example.test' WHERE source_id='00000000-0000-4000-8000-000000000601';
		UPDATE inventory_source_selected_libraries SET remote_library_id='two' WHERE source_id='00000000-0000-4000-8000-000000000601';
		DELETE FROM inventory_source_selected_libraries WHERE source_id='00000000-0000-4000-8000-000000000601';
		-- Temporarily remove only the independent retention guards so the DELETE
		-- branches of the write-lock trigger execute. The transaction rolls back.
		ALTER TABLE inventory_source_configurations DISABLE TRIGGER inventory_source_configurations_retention;
		ALTER TABLE library_sources DISABLE TRIGGER library_sources_configured_inventory_retention;
		DELETE FROM inventory_source_configurations WHERE source_id='00000000-0000-4000-8000-000000000601';
		DELETE FROM library_sources WHERE id='00000000-0000-4000-8000-000000000601';
		ROLLBACK`); err != nil {
		t.Fatalf("direct projection mutations must not reference an unassigned OLD or NEW record: %v", err)
	}
}

func TestAcceptedObservationMigrationsPopulateV25ToV27AndRejectInvalidHistory(t *testing.T) {
	_, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000024_source_configuration_orphan_audit_validation.up.sql")
	seedAcceptedObservationScope(t, ctx, pool)
	v25 := migrationSQL(t, "000025_accepted_inventory_observations.up.sql")
	v26 := migrationSQL(t, "000026_accepted_observation_integrity_remediation.up.sql")
	v27 := migrationSQL(t, "000027_accepted_observation_lineage_integrity.up.sql")
	if _, err := pool.Exec(ctx, v25); err != nil {
		t.Fatalf("apply v25: %v", err)
	}
	if _, err := pool.Exec(ctx, `BEGIN;
		INSERT INTO inventory_accepted_observations(id,source_id,configuration_revision,command_fingerprint,accepted_at) VALUES ('00000000-0000-4000-8000-000000000711','00000000-0000-4000-8000-000000000591',1,decode(repeat('11',32),'hex'),clock_timestamp());
		INSERT INTO inventory_accepted_observation_coverage VALUES ('00000000-0000-4000-8000-000000000711','books','movie','verified_complete');
		INSERT INTO inventory_accepted_observation_items VALUES ('00000000-0000-4000-8000-000000000711','books','item-1','movie','One','','1');
		INSERT INTO inventory_accepted_observation_events VALUES ('00000000-0000-4000-8000-000000000711',1,'added','books','item-1','movie');
		INSERT INTO inventory_desired_source_revision_items VALUES ('00000000-0000-4000-8000-000000000591',1,'books','item-1','movie','One','','1','00000000-0000-4000-8000-000000000711');
		COMMIT`); err != nil {
		t.Fatalf("seed v25 evidence: %v", err)
	}
	if _, err := pool.Exec(ctx, v26); err != nil {
		t.Fatalf("upgrade populated v25 through v26: %v", err)
	}
	if _, err := pool.Exec(ctx, v27); err != nil {
		t.Fatalf("certify populated v25/v26 history in v27: %v", err)
	}
	var acceptanceOrder int
	var lastObservationID string
	if err := pool.QueryRow(ctx, `SELECT observation.acceptance_order, desired.last_observation_id
		FROM inventory_accepted_observations observation
		JOIN inventory_desired_source_revision_items desired
		  ON desired.source_id = observation.source_id
		 AND desired.configuration_revision = observation.configuration_revision
		WHERE observation.id = '00000000-0000-4000-8000-000000000711'`).Scan(&acceptanceOrder, &lastObservationID); err != nil {
		t.Fatalf("read certified v27 history: %v", err)
	}
	if acceptanceOrder != 1 || lastObservationID != "00000000-0000-4000-8000-000000000711" {
		t.Fatalf("certified v27 history = acceptance order %d last observation %q, want order 1 and populated observation", acceptanceOrder, lastObservationID)
	}

	_, _, invalidPool := isolatedDatabase(t)
	applyMigrationsThrough(t, ctx, invalidPool, "000024_source_configuration_orphan_audit_validation.up.sql")
	seedAcceptedObservationScope(t, ctx, invalidPool)
	if _, err := invalidPool.Exec(ctx, v25); err != nil {
		t.Fatalf("apply invalid-history v25: %v", err)
	}
	if _, err := invalidPool.Exec(ctx, `BEGIN;
		INSERT INTO inventory_accepted_observations(id,source_id,configuration_revision,command_fingerprint,accepted_at) VALUES ('00000000-0000-4000-8000-000000000721','00000000-0000-4000-8000-000000000591',1,decode(repeat('12',32),'hex'),clock_timestamp());
		INSERT INTO inventory_accepted_observation_coverage VALUES ('00000000-0000-4000-8000-000000000721','books','movie','verified_complete');
		INSERT INTO inventory_accepted_observation_items VALUES ('00000000-0000-4000-8000-000000000721','books','item-1','movie','One','','1');
		INSERT INTO inventory_desired_source_revision_items VALUES ('00000000-0000-4000-8000-000000000591',1,'books','item-1','movie','One','','1','00000000-0000-4000-8000-000000000721');
		COMMIT`); err != nil {
		t.Fatalf("seed invalid v25 history: %v", err)
	}
	if _, err := invalidPool.Exec(ctx, v26); err != nil {
		t.Fatalf("upgrade invalid v25 through v26: %v", err)
	}
	if _, err := invalidPool.Exec(ctx, v27); err == nil {
		t.Fatal("v27 certified invalid historical transition")
	}
}

func TestAcceptedObservationMigrationsEmptyRollbackReapplyAndPopulatedRollbackRefusal(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	v27Down := migrationSQL(t, "000027_accepted_observation_lineage_integrity.down.sql")
	v27Up := migrationSQL(t, "000027_accepted_observation_lineage_integrity.up.sql")
	v26Down := migrationSQL(t, "000026_accepted_observation_integrity_remediation.down.sql")
	v26Up := migrationSQL(t, "000026_accepted_observation_integrity_remediation.up.sql")
	if _, err := pool.Exec(ctx, v27Down); err != nil {
		t.Fatalf("empty v27 rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, v27Up); err != nil {
		t.Fatalf("empty v27 reapply: %v", err)
	}
	if _, err := pool.Exec(ctx, v27Down); err != nil {
		t.Fatalf("empty v27 rollback before v26: %v", err)
	}
	if _, err := pool.Exec(ctx, v26Down); err != nil {
		t.Fatalf("empty v26 rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, v26Up); err != nil {
		t.Fatalf("empty v26 reapply: %v", err)
	}
	if _, err := pool.Exec(ctx, v27Up); err != nil {
		t.Fatalf("empty v27 reapply after v26: %v", err)
	}
	seedAcceptedObservationScope(t, ctx, pool)
	if _, err := pool.Exec(ctx, `BEGIN;
		INSERT INTO inventory_accepted_observations(id,source_id,configuration_revision,command_fingerprint) VALUES ('00000000-0000-4000-8000-000000000731','00000000-0000-4000-8000-000000000591',1,decode(repeat('13',32),'hex'));
		INSERT INTO inventory_accepted_observation_coverage VALUES ('00000000-0000-4000-8000-000000000731','books','movie','verified_complete');
		INSERT INTO inventory_accepted_observation_items VALUES ('00000000-0000-4000-8000-000000000731','books','item-1','movie','One','','1');
		INSERT INTO inventory_accepted_observation_events VALUES ('00000000-0000-4000-8000-000000000731',1,'added','books','item-1','movie');
		INSERT INTO inventory_desired_source_revision_items VALUES ('00000000-0000-4000-8000-000000000591',1,'books','item-1','movie','One','','1','00000000-0000-4000-8000-000000000731');
		COMMIT`); err != nil {
		t.Fatalf("seed populated v27 evidence: %v", err)
	}
	if _, err := pool.Exec(ctx, v27Down); err == nil {
		t.Fatal("populated v27 rollback unexpectedly succeeded")
	}
}

func migrationSQL(t *testing.T, name string) string {
	t.Helper()
	sql, err := fs.ReadFile(embedded.Files, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(sql)
}

func seedAcceptedObservationScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedCredentialEvidenceSource(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_source_configuration_revision_selected_libraries(source_id,revision,remote_library_id) VALUES ('00000000-0000-4000-8000-000000000591',1,'books')`); err != nil {
		t.Fatalf("seed accepted-observation scope: %v", err)
	}
}

func TestSourceConfigurationHardeningMigrationFromV14ConstraintsRollbackAndReapply(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000014_audiobookshelf_source_configuration.up.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(14,false)`); err != nil {
		t.Fatalf("seed exact v14 history: %v", err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("upgrade exact v14 history to v15: %v", err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("repeat v15 upgrade: %v", err)
	}

	assertSourceConfigurationHardeningSchema(t, ctx, pool, schema, true)
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,authoritative_media_types,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000402','audiobookshelf','Hardening source','["audiobook"]',clock_timestamp(),clock_timestamp())`); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO inventory_source_configuration_audit(id,source_id,action,configuration_revision,command_fingerprint,occurred_at,credential_identity,manual_intent_replayed,manual_intent_coalesced) VALUES ('00000000-0000-4000-8000-000000000401','00000000-0000-4000-8000-000000000402','configured',1,$1,clock_timestamp(),$1,TRUE,TRUE)`, bytesOf(1, 32))
	assertConstraintFailure(t, ctx, pool, `INSERT INTO inventory_source_configuration_audit(id,source_id,action,configuration_revision,command_fingerprint,occurred_at,credential_identity) VALUES ('00000000-0000-4000-8000-000000000403','00000000-0000-4000-8000-000000000402','configured',1,$1,clock_timestamp(),$2)`, bytesOf(1, 32), bytesOf(2, 31))
	if _, err := pool.Exec(ctx, `INSERT INTO encryption_key_registry(singleton,active_key_id) VALUES (TRUE,$1)`, bytesOf(3, 16)); err != nil {
		t.Fatalf("insert encryption key: %v", err)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO encrypted_secrets(namespace,owner_id,secret_name,envelope_version,key_id,nonce,ciphertext,created_at,updated_at,credential_identity) VALUES ('unrelated','00000000-0000-4000-8000-000000000402','test',1,$1,$2,$3,clock_timestamp(),clock_timestamp(),$4)`, bytesOf(3, 16), bytesOf(4, 12), bytesOf(5, 16), bytesOf(6, 31))

	down, err := fs.ReadFile(embedded.Files, "000015_source_configuration_hardening.down.sql")
	if err != nil {
		t.Fatalf("read v15 down migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("clean v15 rollback: %v", err)
	}
	assertSourceConfigurationHardeningSchema(t, ctx, pool, schema, false)
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=14, dirty=false`); err != nil {
		t.Fatalf("reset local/test migration history: %v", err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v15: %v", err)
	}
	assertSourceConfigurationHardeningSchema(t, ctx, pool, schema, true)
}

func TestSourceConfigurationHardeningRollbackRefusesRecordedSourceState(t *testing.T) {
	tests := []struct {
		name    string
		seed    string
		wantErr string
	}{
		{name: "configuration", seed: `INSERT INTO inventory_source_configurations(source_id,endpoint_url,updated_at) VALUES ('00000000-0000-4000-8000-000000000411','https://abs.example',clock_timestamp())`, wantErr: "inventory source configurations exist"},
		{name: "configuration audit", seed: `INSERT INTO inventory_source_configuration_audit(id,source_id,action,configuration_revision,command_fingerprint,occurred_at) VALUES ('00000000-0000-4000-8000-000000000412','00000000-0000-4000-8000-000000000411','configured',1,decode(repeat('01',32),'hex'),clock_timestamp())`, wantErr: "configuration audit evidence exists"},
		{name: "manual sync intent", seed: `INSERT INTO inventory_manual_sync_intents(id,source_id,requested_at) VALUES ('00000000-0000-4000-8000-000000000413','00000000-0000-4000-8000-000000000411',clock_timestamp())`, wantErr: "manual sync intents exist"},
		{name: "source credential", seed: `INSERT INTO encryption_key_registry(singleton,active_key_id) VALUES (TRUE,decode(repeat('02',16),'hex')); INSERT INTO encrypted_secrets(namespace,owner_id,secret_name,envelope_version,key_id,nonce,ciphertext,created_at,updated_at) VALUES ('inventory-source','00000000-0000-4000-8000-000000000411','credential',1,decode(repeat('02',16),'hex'),decode(repeat('03',12),'hex'),decode(repeat('04',16),'hex'),clock_timestamp(),clock_timestamp())`, wantErr: "inventory source credentials exist"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			databaseURL, schema, pool := isolatedDatabase(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := Up(ctx, databaseURL); err != nil {
				t.Fatalf("apply through v15: %v", err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,authoritative_media_types,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000411','audiobookshelf','Test source','["audiobook"]',clock_timestamp(),clock_timestamp())`); err != nil {
				t.Fatalf("insert source: %v", err)
			}
			if _, err := pool.Exec(ctx, tt.seed); err != nil {
				t.Fatalf("seed %s state: %v", tt.name, err)
			}
			down, err := fs.ReadFile(embedded.Files, "000015_source_configuration_hardening.down.sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, string(down)); err == nil || !regexp.MustCompile(regexp.QuoteMeta(tt.wantErr)).MatchString(err.Error()) {
				t.Fatalf("v15 rollback error = %v, want %q", err, tt.wantErr)
			}
			assertSourceConfigurationHardeningSchema(t, ctx, pool, schema, true)
		})
	}
}

func TestSourceConfigurationAuditSelectedLibrariesMigrationRollbackAndRetention(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000015_source_configuration_hardening.up.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(15,false)`); err != nil {
		t.Fatalf("seed exact v15 history: %v", err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("upgrade exact v15 history to v16: %v", err)
	}
	assertTableExists(t, ctx, pool, schema, "inventory_source_configuration_audit_selected_libraries", true)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO inventory_source_configuration_audit_selected_libraries(audit_id,remote_library_id) VALUES ('00000000-0000-4000-8000-000000000451','')`)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO inventory_source_configuration_audit_selected_libraries(audit_id,remote_library_id) VALUES ('00000000-0000-4000-8000-000000000451',repeat('x',257))`)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO inventory_source_configuration_audit_selected_libraries(audit_id,remote_library_id) VALUES ('00000000-0000-4000-8000-000000000451','fiction')`)

	down, err := fs.ReadFile(embedded.Files, "000016_source_configuration_audit_selected_libraries.down.sql")
	if err != nil {
		t.Fatalf("read v16 down migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("clean v16 rollback: %v", err)
	}
	assertTableExists(t, ctx, pool, schema, "inventory_source_configuration_audit_selected_libraries", false)
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=15, dirty=false`); err != nil {
		t.Fatalf("reset local/test migration history: %v", err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v16: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,authoritative_media_types,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000452','audiobookshelf','Evidence source','["audiobook"]',clock_timestamp(),clock_timestamp()); INSERT INTO inventory_source_configuration_audit(id,source_id,action,configuration_revision,command_fingerprint,occurred_at,credential_identity) VALUES ('00000000-0000-4000-8000-000000000451','00000000-0000-4000-8000-000000000452','configured',1,decode(repeat('01',32),'hex'),clock_timestamp(),decode(repeat('02',32),'hex')); INSERT INTO inventory_source_configuration_audit_selected_libraries(audit_id,remote_library_id) VALUES ('00000000-0000-4000-8000-000000000451','fiction')`); err != nil {
		t.Fatalf("seed audit library evidence: %v", err)
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil || !regexp.MustCompile(`audit library evidence exists`).MatchString(err.Error()) {
		t.Fatalf("v16 rollback error = %v, want audit library evidence refusal", err)
	}
	assertTableExists(t, ctx, pool, schema, "inventory_source_configuration_audit_selected_libraries", true)
}

func TestManualSyncRequestAttemptsMigrationRollbackAndRetention(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000016_source_configuration_audit_selected_libraries.up.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(16,false)`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("upgrade exact v16 history to v17: %v", err)
	}
	assertTableExists(t, ctx, pool, schema, "inventory_manual_sync_request_attempts", true)
	down, err := fs.ReadFile(embedded.Files, "000017_manual_sync_request_attempts.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("clean v17 rollback: %v", err)
	}
	assertTableExists(t, ctx, pool, schema, "inventory_manual_sync_request_attempts", false)
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=16, dirty=false`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v17: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,authoritative_media_types,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000462','audiobookshelf','Evidence source','["audiobook"]',clock_timestamp(),clock_timestamp()); INSERT INTO inventory_manual_sync_intents(id,source_id,requested_at) VALUES ('00000000-0000-4000-8000-000000000463','00000000-0000-4000-8000-000000000462',clock_timestamp()); INSERT INTO inventory_manual_sync_request_attempts(request_id,source_id,requested_at,disposition,pending_intent_id) VALUES ('00000000-0000-4000-8000-000000000464','00000000-0000-4000-8000-000000000462',clock_timestamp(),'recorded','00000000-0000-4000-8000-000000000463')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM inventory_manual_sync_request_attempts WHERE request_id='00000000-0000-4000-8000-000000000464'`); err == nil {
		t.Fatal("direct attempt evidence deletion unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil || !regexp.MustCompile(`manual sync request attempt evidence exists`).MatchString(err.Error()) {
		t.Fatalf("v17 rollback error=%v", err)
	}
	assertTableExists(t, ctx, pool, schema, "inventory_manual_sync_request_attempts", true)
	var triggerExists, uniqueIndexExists bool
	if err := pool.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid='inventory_manual_sync_request_attempts'::regclass AND tgname='inventory_manual_sync_request_attempts_append_only' AND NOT tgisinternal),
		EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname=current_schema() AND tablename='inventory_manual_sync_request_attempts' AND indexname='inventory_manual_sync_request_attempts_request_initial_idx')`).Scan(&triggerExists, &uniqueIndexExists); err != nil {
		t.Fatal(err)
	}
	if !triggerExists || !uniqueIndexExists {
		t.Fatalf("refused v17 rollback removed protections: trigger/index=%t/%t", triggerExists, uniqueIndexExists)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO inventory_manual_sync_request_attempts(request_id,source_id,requested_at,disposition,pending_intent_id) VALUES ('00000000-0000-4000-8000-000000000464','00000000-0000-4000-8000-000000000462',clock_timestamp(),'coalesced','00000000-0000-4000-8000-000000000463')`)
	if _, err := pool.Exec(ctx, `UPDATE inventory_manual_sync_request_attempts SET disposition='replayed' WHERE request_id='00000000-0000-4000-8000-000000000464'`); err == nil {
		t.Fatal("refused v17 rollback left append-only trigger ineffective")
	}
}

func TestSourceConfigurationRevisionEvidenceMigrationRollbackAndRetention(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000017_manual_sync_request_attempts.up.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(17,false)`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("upgrade exact v17 history to v18: %v", err)
	}
	assertTableExists(t, ctx, pool, schema, "inventory_source_configuration_revisions", true)
	assertTableExists(t, ctx, pool, schema, "inventory_source_configuration_revision_selected_libraries", true)
	down, err := fs.ReadFile(embedded.Files, "000018_source_configuration_revision_evidence.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("clean v18 rollback: %v", err)
	}
	assertTableExists(t, ctx, pool, schema, "inventory_source_configuration_revisions", false)
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=17, dirty=false`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v18: %v", err)
	}
	if _, err := pool.Exec(ctx, `BEGIN; INSERT INTO library_sources(id,kind,name,authoritative_media_types,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000471','plex','Evidence source','["movie"]',clock_timestamp(),clock_timestamp()); INSERT INTO inventory_source_configurations(source_id,endpoint_url,updated_at) VALUES ('00000000-0000-4000-8000-000000000471','https://plex.example.test',clock_timestamp()); INSERT INTO encryption_key_registry(singleton,active_key_id) VALUES (TRUE,decode(repeat('01',16),'hex')); INSERT INTO encrypted_secrets(namespace,owner_id,secret_name,envelope_version,key_id,nonce,ciphertext,credential_identity,created_at,updated_at) VALUES ('inventory-source','00000000-0000-4000-8000-000000000471','credential',1,decode(repeat('01',16),'hex'),decode(repeat('02',12),'hex'),decode(repeat('03',16),'hex'),decode(repeat('02',32),'hex'),clock_timestamp(),clock_timestamp()); INSERT INTO inventory_source_configuration_audit(id,source_id,action,configuration_revision,command_fingerprint,occurred_at,credential_identity) VALUES ('00000000-0000-4000-8000-000000000472','00000000-0000-4000-8000-000000000471','configured',1,decode(repeat('01',32),'hex'),clock_timestamp(),decode(repeat('02',32),'hex')); INSERT INTO inventory_source_configuration_revisions(source_id,revision,source_kind,source_name,endpoint_url,enabled,enabled_media_types,credential_identity,audit_id,recorded_at) VALUES ('00000000-0000-4000-8000-000000000471',1,'plex','Evidence source','https://plex.example.test',true,'["movie"]',decode(repeat('02',32),'hex'),'00000000-0000-4000-8000-000000000472',clock_timestamp()); INSERT INTO inventory_source_configuration_revision_selected_libraries(source_id,revision,remote_library_id) VALUES ('00000000-0000-4000-8000-000000000471',1,'1'); COMMIT`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_source_configuration_revisions SET source_name='changed' WHERE source_id='00000000-0000-4000-8000-000000000471'`); err == nil {
		t.Fatal("direct revision evidence mutation unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil || !regexp.MustCompile(`revision evidence exists`).MatchString(err.Error()) {
		t.Fatalf("v18 rollback error=%v", err)
	}
	assertTableExists(t, ctx, pool, schema, "inventory_source_configuration_revisions", true)
}

func TestSourceConfigurationProjectionIntegrityMigrationRollbackRefusesPlexEvidence(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `BEGIN; INSERT INTO library_sources(id,kind,name,enabled,authoritative_media_types,created_at,updated_at)
		VALUES ('00000000-0000-4000-8000-000000000481','plex','Plex',true,'["movie"]',clock_timestamp(),clock_timestamp());
		INSERT INTO inventory_source_configurations(source_id,endpoint_url,revision,updated_at)
		VALUES ('00000000-0000-4000-8000-000000000481','https://plex.example.test',1,clock_timestamp());
		INSERT INTO encryption_key_registry(singleton,active_key_id) VALUES (TRUE,decode(repeat('01',16),'hex'));
		INSERT INTO encrypted_secrets(namespace,owner_id,secret_name,envelope_version,key_id,nonce,ciphertext,credential_identity,created_at,updated_at)
		VALUES ('inventory-source','00000000-0000-4000-8000-000000000481','credential',1,decode(repeat('01',16),'hex'),decode(repeat('02',12),'hex'),decode(repeat('03',16),'hex'),decode(repeat('04',32),'hex'),clock_timestamp(),clock_timestamp());
		INSERT INTO inventory_source_configuration_audit(id,source_id,action,configuration_revision,command_fingerprint,credential_identity,occurred_at)
		VALUES ('00000000-0000-4000-8000-000000000482','00000000-0000-4000-8000-000000000481','configured',1,decode(repeat('05',32),'hex'),decode(repeat('04',32),'hex'),clock_timestamp());
		INSERT INTO inventory_source_configuration_revisions(source_id,revision,source_kind,source_name,endpoint_url,enabled,enabled_media_types,credential_identity,audit_id,recorded_at)
		VALUES ('00000000-0000-4000-8000-000000000481',1,'plex','Plex','https://plex.example.test',true,'["movie"]',decode(repeat('04',32),'hex'),'00000000-0000-4000-8000-000000000482',clock_timestamp()); COMMIT`); err != nil {
		t.Fatalf("seed Plex configuration evidence: %v", err)
	}
	down, err := fs.ReadFile(embedded.Files, "000019_source_configuration_projection_integrity.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil || !regexp.MustCompile(`Plex source configuration projection or revision evidence exists`).MatchString(err.Error()) {
		t.Fatalf("v19 rollback error=%v, want Plex rollback refusal", err)
	}
}

func TestCredentialEnvelopeEvidenceMigrationUpgradeFromV19IntegrityAndRollback(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000019_source_configuration_projection_integrity.up.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(19,false)`); err != nil {
		t.Fatal(err)
	}
	seedCredentialEvidenceSource(t, ctx, pool)
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("upgrade populated 000019 history: %v", err)
	}
	var version int
	if err := pool.QueryRow(ctx, `SELECT version FROM schema_migrations`).Scan(&version); err != nil || version != 23 {
		t.Fatalf("upgrade from v19 history reached version %d with err %v, want v23", version, err)
	}

	assertConstraintFailure(t, ctx, pool, `UPDATE encrypted_secrets SET namespace='other' WHERE namespace='inventory-source' AND secret_name='credential'`)
	assertConstraintFailure(t, ctx, pool, `UPDATE encrypted_secrets SET secret_name='other' WHERE namespace='inventory-source' AND secret_name='credential'`)
	assertConstraintFailure(t, ctx, pool, `UPDATE encrypted_secrets SET owner_id='00000000-0000-4000-8000-000000000592' WHERE namespace='inventory-source' AND secret_name='credential'`)
	assertConstraintFailure(t, ctx, pool, `UPDATE inventory_source_configuration_credential_revisions SET nonce=decode(repeat('09',12),'hex')`)
	assertConstraintFailure(t, ctx, pool, `DELETE FROM inventory_source_configuration_credential_revisions`)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO inventory_source_configuration_credential_revisions(source_id,revision,envelope_version,key_id,nonce,ciphertext,credential_identity) VALUES ('00000000-0000-4000-8000-000000000591',2,1,decode(repeat('01',16),'hex'),decode(repeat('02',12),'hex'),decode(repeat('03',16),'hex'),decode(repeat('09',32),'hex'))`)

	down, err := fs.ReadFile(embedded.Files, "000021_source_configuration_credential_evidence_integrity.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil || !regexp.MustCompile(`credential envelope evidence exists`).MatchString(err.Error()) {
		t.Fatalf("populated v21 rollback error=%v", err)
	}
}

func TestCredentialEnvelopeEvidenceMigrationEmptyRollbackAndReapply(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	down, err := fs.ReadFile(embedded.Files, "000021_source_configuration_credential_evidence_integrity.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("empty v21 rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=20, dirty=false`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v21: %v", err)
	}
}

func seedCredentialEvidenceSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `BEGIN;
		INSERT INTO library_sources(id,kind,name,enabled,authoritative_media_types,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000591','plex','Plex',true,'["movie"]',clock_timestamp(),clock_timestamp());
		INSERT INTO inventory_source_configurations(source_id,endpoint_url,revision,updated_at) VALUES ('00000000-0000-4000-8000-000000000591','https://plex.example.test',1,clock_timestamp());
		INSERT INTO encryption_key_registry(singleton,active_key_id) VALUES (TRUE,decode(repeat('01',16),'hex'));
		INSERT INTO encrypted_secrets(namespace,owner_id,secret_name,envelope_version,key_id,nonce,ciphertext,credential_identity,created_at,updated_at) VALUES ('inventory-source','00000000-0000-4000-8000-000000000591','credential',1,decode(repeat('01',16),'hex'),decode(repeat('02',12),'hex'),decode(repeat('03',16),'hex'),decode(repeat('04',32),'hex'),clock_timestamp(),clock_timestamp());
		INSERT INTO inventory_source_configuration_audit(id,source_id,action,configuration_revision,command_fingerprint,credential_identity,occurred_at) VALUES ('00000000-0000-4000-8000-000000000593','00000000-0000-4000-8000-000000000591','configured',1,decode(repeat('05',32),'hex'),decode(repeat('04',32),'hex'),clock_timestamp());
		INSERT INTO inventory_source_configuration_revisions(source_id,revision,source_kind,source_name,endpoint_url,enabled,enabled_media_types,credential_identity,audit_id,recorded_at) VALUES ('00000000-0000-4000-8000-000000000591',1,'plex','Plex','https://plex.example.test',true,'["movie"]',decode(repeat('04',32),'hex'),'00000000-0000-4000-8000-000000000593',clock_timestamp());
		COMMIT`)
	if err != nil {
		t.Fatalf("seed populated configuration evidence: %v", err)
	}
}

func TestInventoryCyclesAndDurableJobsMigrationRollbackSafety(t *testing.T) {
	_, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000012_runtime_auth_activation.up.sql")
	v13, err := fs.ReadFile(embedded.Files, "000013_inventory_cycles_durable_jobs.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(v13)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	assertConstraintFailure(t, ctx, pool, `INSERT INTO durable_jobs(id,idempotency_key,kind,payload,attempt,state,available_at,created_at,leased_at,lease_token,lease_expires_at) VALUES('00000000-0000-4000-8000-000000000304','00000000-0000-4000-8000-000000000305','inventory.sync','',1,'leased',$1,$1,$1,'00000000-0000-4000-8000-000000000306',$1 + interval '1 hour 1 second')`, now)
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,authoritative_media_types,created_at,updated_at) VALUES('00000000-0000-4000-8000-000000000307','plex','Plex validation','https://plex.example','["movie"]',$1,$1); INSERT INTO inventory_authoritative_cycles(id,source_id,started_at,completed_at,committed_at) VALUES('00000000-0000-4000-8000-000000000308','00000000-0000-4000-8000-000000000307',$1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO inventory_authoritative_cycle_events(cycle_id,source_item_id,change_kind,media_type) VALUES('00000000-0000-4000-8000-000000000308',repeat('x',257),'added','movie')`)
	if _, err := pool.Exec(ctx, `DELETE FROM inventory_authoritative_cycles WHERE id='00000000-0000-4000-8000-000000000308'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO durable_jobs(id,idempotency_key,kind,payload,attempt,state,available_at,created_at) VALUES('00000000-0000-4000-8000-000000000301','00000000-0000-4000-8000-000000000302','inventory.sync','',1,'queued',$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	down, err := fs.ReadFile(embedded.Files, "000013_inventory_cycles_durable_jobs.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("v13 rollback silently removed durable queue history")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM durable_jobs`); err != nil {
		t.Fatal(err)
	}
	now = time.Now().UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,authoritative_media_types,created_at,updated_at) VALUES('00000000-0000-4000-8000-000000000303','plex','Plex','https://plex.example','["movie"]',$1,$1); INSERT INTO inventory_source_status(source_id,state) VALUES('00000000-0000-4000-8000-000000000303','idle')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("v13 rollback silently removed source-status state")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM inventory_source_status`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("empty v13 rollback: %v", err)
	}
}

func TestRuntimeAuthActivationMigrationConstraintsRollbackAndReapply(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000010_audiobookshelf_inventory_snapshots.up.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(10,false)`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	assertTableExists(t, ctx, pool, schema, "auth_runtime_activation", true)
	now := time.Now().UTC()
	assertConstraintFailure(t, ctx, pool, `INSERT INTO auth_runtime_activation(singleton,authentication_revision,activated_at) VALUES(FALSE,1,$1)`, now)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO auth_runtime_activation(singleton,authentication_revision,activated_at) VALUES(TRUE,0,$1)`, now)
	if _, err := pool.Exec(ctx, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES('authentication_activated',$1)`, now); err != nil {
		t.Fatal(err)
	}
	down, err := fs.ReadFile(embedded.Files, "000012_runtime_auth_activation.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("v12 rollback silently removed activation history")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM setup_workflow_events WHERE event_type='authentication_activated'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("clean v12 rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=11,dirty=false`); err != nil {
		t.Fatal(err)
	}
	assertTableExists(t, ctx, pool, schema, "auth_runtime_activation", false)
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v12: %v", err)
	}
	assertTableExists(t, ctx, pool, schema, "auth_runtime_activation", true)
}

func TestNormalAuthFoundationMigrationConstraintsAndRollbackSafety(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	userID := "00000000-0000-4000-8000-000000000011"
	loginID := "00000000-0000-4000-8000-000000000012"
	sessionID := "00000000-0000-4000-8000-000000000013"
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, oidc_issuer, oidc_subject, display_name, role, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$6)`, userID, "https://issuer.example", "subject", "User", "viewer", now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, oidc_issuer, oidc_subject, display_name, role, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$6)`, "00000000-0000-4000-8000-000000000014", "https://second.example", "subject", "Other", "requester", now); err != nil {
		t.Fatalf("same subject at another issuer: %v", err)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO users (id, oidc_issuer, oidc_subject, display_name, role, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$6)`, "00000000-0000-4000-8000-000000000015", "https://issuer.example", "subject", "Duplicate", "viewer", now)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO auth_login_transactions VALUES ($1,$2,$3,$4,1,$5,$5,NULL)`, loginID, make([]byte, 32), make([]byte, 32), make([]byte, 32), now)
	if _, err := pool.Exec(ctx, `INSERT INTO auth_login_transactions VALUES ($1,$2,$3,$4,1,$5,$6,NULL)`, loginID, make([]byte, 32), append([]byte{1}, make([]byte, 31)...), append([]byte{2}, make([]byte, 31)...), now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO auth_sessions VALUES ($1,$2,$3,1,$4,$5,NULL)`, sessionID, make([]byte, 32), userID, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO auth_sessions VALUES ($1,$2,$3,1,$4,$5,NULL)`, "00000000-0000-4000-8000-000000000018", make([]byte, 31), userID, now, now.Add(time.Hour))
	assertConstraintFailure(t, ctx, pool, `INSERT INTO auth_sessions VALUES ($1,$2,$3,1,$4,$5,$6)`, "00000000-0000-4000-8000-000000000019", append([]byte{3}, make([]byte, 31)...), userID, now, now.Add(time.Hour), now.Add(-time.Second))
	assertConstraintFailure(t, ctx, pool, `INSERT INTO auth_runtime_config(singleton,authentication_revision,issuer,client_id,client_type,scopes,groups_claim,default_role,activated_at) VALUES (TRUE,1,'https://issuer.example','requestarr','public','[]'::jsonb,'groups','viewer',$1)`, now)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO auth_audit_events(id,event_type,data,created_at) VALUES ($1,'unknown','{}'::jsonb,$2)`, "00000000-0000-4000-8000-000000000020", now)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO auth_audit_events(id,event_type,data,created_at) VALUES ($1,'login_failed','[]'::jsonb,$2)`, "00000000-0000-4000-8000-000000000021", now)
	down, err := fs.ReadFile(embedded.Files, "000011_normal_auth_foundation.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("v11 rollback silently removed normal-auth history")
	}
}

func TestNormalAuthFoundationRejectsLegacyUsersWithoutSchemaOrHistoryChange(t *testing.T) {
	_, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000010_audiobookshelf_inventory_snapshots.up.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(10,false)`); err != nil {
		t.Fatalf("seed exact v10 history: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, oidc_subject, display_name, role, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$5)`, "00000000-0000-4000-8000-000000000016", "legacy-subject", "Legacy", "viewer", now); err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}

	v11, err := fs.ReadFile(embedded.Files, "000011_normal_auth_foundation.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(v11)); err == nil {
		t.Fatal("v11 accepted a legacy user without issuer provenance")
	}
	for _, table := range []string{"auth_runtime_config", "auth_login_transactions", "auth_sessions", "auth_audit_events"} {
		assertTableExists(t, ctx, pool, schema, table, false)
	}
	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil || version != 10 || dirty {
		t.Fatalf("migration history after rejected v11 = version %d dirty %t err=%v, want version 10 clean", version, dirty, err)
	}
	var issuerColumn bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=$1 AND table_name='users' AND column_name='oidc_issuer')`, schema).Scan(&issuerColumn); err != nil || issuerColumn {
		t.Fatalf("issuer column after rejected v11 = %t err=%v, want absent", issuerColumn, err)
	}
}

func TestNormalAuthFoundationEmptyHistoryRollbackAndReapply(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigrationsThrough(t, ctx, pool, "000010_audiobookshelf_inventory_snapshots.up.sql")
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(10,false)`); err != nil {
		t.Fatalf("seed exact v10 history: %v", err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("upgrade empty v10 history to v11: %v", err)
	}
	down, err := fs.ReadFile(embedded.Files, "000011_normal_auth_foundation.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("rollback empty v11 history: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=10, dirty=false`); err != nil {
		t.Fatalf("reset local/test migration history: %v", err)
	}
	for _, table := range []string{"auth_runtime_config", "auth_login_transactions", "auth_sessions", "auth_audit_events"} {
		assertTableExists(t, ctx, pool, schema, table, false)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v11: %v", err)
	}
	for _, table := range []string{"auth_runtime_config", "auth_login_transactions", "auth_sessions", "auth_audit_events"} {
		assertTableExists(t, ctx, pool, schema, table, true)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO users (id, oidc_issuer, oidc_subject, display_name, role, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,clock_timestamp(),clock_timestamp())`, "00000000-0000-4000-8000-000000000017", "", "subject", "User", "viewer")
}

func TestOIDCValidationMigrationBackfillConstraintsAndRollbackSafety(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, name := range []string{"000001_initial.up.sql", "000002_setup_bootstrap_sessions.up.sql", "000003_setup_workflow_events.up.sql", "000004_encrypted_secrets.up.sql", "000005_setup_authentication_saved.up.sql"} {
		migration, err := fs.ReadFile(embedded.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	draft := `{"version":2,"authentication":{"issuer":"https://issuer.example","client_id":"requestarr","client_type":"public","scopes":["openid"],"groups_claim":"groups","default_role":"viewer","group_role_mappings":[]}}`
	if _, err := pool.Exec(ctx, `UPDATE setup_state SET draft=$1::jsonb`, draft); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY,dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(5,false)`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT authentication_revision FROM setup_state`).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("backfilled revision=%d err=%v", revision, err)
	}
	now := time.Now().UTC()
	assertConstraintFailure(t, ctx, pool, `UPDATE setup_state SET authentication_revision=-1`)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_authentication_validation_evidence VALUES(TRUE,0,1,$1,$2)`, make([]byte, 32), now)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_authentication_validation_evidence VALUES(TRUE,1,1,$1,$2)`, make([]byte, 31), now)
	if _, err := pool.Exec(ctx, `INSERT INTO setup_authentication_validation_evidence VALUES(TRUE,1,1,$1,$2)`, make([]byte, 32), now); err != nil {
		t.Fatal(err)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_authentication_validation_rate_limits VALUES('other',$1,$2,1,$2)`, make([]byte, 32), now)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_authentication_validation_rate_limits VALUES('session',$1,$2,0,$2)`, make([]byte, 32), now)
	if _, err := pool.Exec(ctx, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES('authentication_validation_succeeded',$1)`, now); err != nil {
		t.Fatal(err)
	}
	down, err := fs.ReadFile(embedded.Files, "000006_oidc_discovery_validation.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("v6 rollback silently removed validation history")
	}
}

func TestOIDCValidationMigrationEmptyHistoryRollbackReapplyAndZeroBackfill(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, name := range []string{"000001_initial.up.sql", "000002_setup_bootstrap_sessions.up.sql", "000003_setup_workflow_events.up.sql", "000004_encrypted_secrets.up.sql", "000005_setup_authentication_saved.up.sql"} {
		migration, err := fs.ReadFile(embedded.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE setup_state SET draft='{"version":2}'::jsonb; CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY,dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES(5,false)`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT authentication_revision FROM setup_state`).Scan(&revision); err != nil || revision != 0 {
		t.Fatalf("non-authentication backfill revision=%d err=%v", revision, err)
	}
	down, err := fs.ReadFile(embedded.Files, "000006_oidc_discovery_validation.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("empty-history rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=5,dirty=false`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"setup_authentication_validation_evidence", "setup_authentication_validation_rate_limits"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil || exists {
			t.Fatalf("rolled-back table %s exists=%t err=%v", table, exists, err)
		}
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v6: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT authentication_revision FROM setup_state`).Scan(&revision); err != nil || revision != 0 {
		t.Fatalf("reapplied revision=%d err=%v", revision, err)
	}
}

func TestEncryptedSecretsMigrationFromExactV3AndRollbackReapply(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, name := range []string{"000001_initial.up.sql", "000002_setup_bootstrap_sessions.up.sql", "000003_setup_workflow_events.up.sql"} {
		migration, err := fs.ReadFile(embedded.Files, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES (3, false)`); err != nil {
		t.Fatalf("seed exact v3 history: %v", err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("migrate exact v3 to v4: %v", err)
	}
	assertEncryptedSecretsSchema(t, ctx, pool, schema)

	down, err := fs.ReadFile(embedded.Files, "000004_encrypted_secrets.down.sql")
	if err != nil {
		t.Fatalf("read v4 down migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("dependency-safe v4 rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=3, dirty=false`); err != nil {
		t.Fatalf("reset local/test migration history: %v", err)
	}
	for _, table := range []string{"encrypted_secrets", "encryption_key_registry"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil || exists {
			t.Fatalf("rolled-back table %s exists=%t err=%v", table, exists, err)
		}
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v4 after rollback: %v", err)
	}
	assertEncryptedSecretsSchema(t, ctx, pool, schema)
}

func TestAuthenticationAuditMigrationExactV4RollbackSafety(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, name := range []string{"000001_initial.up.sql", "000002_setup_bootstrap_sessions.up.sql", "000003_setup_workflow_events.up.sql", "000004_encrypted_secrets.up.sql"} {
		migration, err := fs.ReadFile(embedded.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES (4, false)`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("exact v4 to v5: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES ('authentication_saved',clock_timestamp())`); err != nil {
		t.Fatalf("authentication event not allowlisted: %v", err)
	}
	down, err := fs.ReadFile(embedded.Files, "000005_setup_authentication_saved.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("v5 rollback deleted authentication audit history")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM setup_workflow_events WHERE event_type='authentication_saved'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("clean v5 rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=4,dirty=false`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v5: %v", err)
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES ('not_allowed',clock_timestamp())`)
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

func TestEncryptedSecretsMigrationConstraints(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-4000-8000-000000000001"
	now := time.Now().UTC()
	keyID := make([]byte, 16)
	keyID[0] = 1
	if _, err := pool.Exec(ctx, `INSERT INTO encryption_key_registry(singleton,active_key_id) VALUES (TRUE,$1)`, keyID); err != nil {
		t.Fatalf("register active key: %v", err)
	}
	insert := `INSERT INTO encrypted_secrets (namespace,owner_id,secret_name,envelope_version,key_id,nonce,ciphertext,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	if _, err := pool.Exec(ctx, insert, "oidc", id, "client_secret", 1, keyID, make([]byte, 12), make([]byte, 16), now, now); err != nil {
		t.Fatalf("valid encrypted row: %v", err)
	}
	assertConstraintFailure(t, ctx, pool, insert, "oidc", id, "unregistered", 1, make([]byte, 16), make([]byte, 12), make([]byte, 16), now, now)
	assertConstraintFailure(t, ctx, pool, insert, "oidc", id, "other", 2, keyID, make([]byte, 12), make([]byte, 16), now, now)
	assertConstraintFailure(t, ctx, pool, insert, "oidc", id, "other", 1, make([]byte, 15), make([]byte, 12), make([]byte, 16), now, now)
	assertConstraintFailure(t, ctx, pool, insert, "oidc", id, "other", 1, keyID, make([]byte, 11), make([]byte, 16), now, now)
	assertConstraintFailure(t, ctx, pool, insert, "oidc", id, "other", 1, keyID, make([]byte, 12), make([]byte, 15), now, now)
	assertConstraintFailure(t, ctx, pool, insert, "", id, "other", 1, keyID, make([]byte, 12), make([]byte, 16), now, now)
	assertConstraintFailure(t, ctx, pool, insert, "oidc", id, "", 1, keyID, make([]byte, 12), make([]byte, 16), now, now)
	assertConstraintFailure(t, ctx, pool, insert, "oidc", id, "client_secret", 1, keyID, make([]byte, 12), make([]byte, 16), now, now)

	assertConstraintFailure(t, ctx, pool, `INSERT INTO encryption_key_registry(singleton,active_key_id) VALUES (FALSE,$1)`, bytesOf(2, 16))
	assertConstraintFailure(t, ctx, pool, `INSERT INTO encryption_key_registry(singleton,active_key_id) VALUES (TRUE,$1)`, bytesOf(2, 16))
	assertConstraintFailure(t, ctx, pool, `UPDATE encryption_key_registry SET active_key_id=$1`, make([]byte, 15))
}

func assertEncryptedSecretsSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string) {
	t.Helper()
	for _, table := range []string{"encryption_key_registry", "encrypted_secrets"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil || !exists {
			t.Fatalf("expected table %s: exists=%t err=%v", table, exists, err)
		}
	}
	assertConstraintFailure(t, ctx, pool, `INSERT INTO encryption_key_registry(singleton,active_key_id) VALUES (FALSE,$1)`, make([]byte, 16))
	assertConstraintFailure(t, ctx, pool, `INSERT INTO encryption_key_registry(singleton,active_key_id) VALUES (TRUE,$1)`, make([]byte, 15))
}

func bytesOf(value byte, size int) []byte {
	result := make([]byte, size)
	for i := range result {
		result[i] = value
	}
	return result
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

func applyMigrationsThrough(t *testing.T, ctx context.Context, pool *pgxpool.Pool, last string) {
	t.Helper()
	for _, name := range []string{
		"000001_initial.up.sql",
		"000002_setup_bootstrap_sessions.up.sql",
		"000003_setup_workflow_events.up.sql",
		"000004_encrypted_secrets.up.sql",
		"000005_setup_authentication_saved.up.sql",
		"000006_oidc_discovery_validation.up.sql",
		"000007_audiobook_catalog_sources.up.sql",
		"000008_reconciliation_match_evidence.up.sql",
		"000009_reconciliation_audit.up.sql",
		"000010_audiobookshelf_inventory_snapshots.up.sql",
		"000011_normal_auth_foundation.up.sql",
		"000012_runtime_auth_activation.up.sql",
		"000013_inventory_cycles_durable_jobs.up.sql",
		"000014_audiobookshelf_source_configuration.up.sql",
		"000015_source_configuration_hardening.up.sql",
		"000016_source_configuration_audit_selected_libraries.up.sql",
		"000017_manual_sync_request_attempts.up.sql",
		"000018_source_configuration_revision_evidence.up.sql",
		"000019_source_configuration_projection_integrity.up.sql",
		"000020_source_configuration_credential_envelope_evidence.up.sql",
		"000021_source_configuration_credential_evidence_integrity.up.sql",
		"000022_source_configuration_write_lock_safety.up.sql",
		"000023_dirty_v20_recovery_audit_integrity.up.sql",
		"000024_source_configuration_orphan_audit_validation.up.sql",
	} {
		migration, err := fs.ReadFile(embedded.Files, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if name == last {
			return
		}
	}
	t.Fatalf("requested migration %s is not available", last)
}

func assertSourceConfigurationHardeningSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string, want bool) {
	t.Helper()
	for _, check := range []struct {
		table  string
		column string
	}{
		{table: "inventory_source_configuration_audit", column: "credential_identity"},
		{table: "inventory_source_configuration_audit", column: "manual_intent_replayed"},
		{table: "inventory_source_configuration_audit", column: "manual_intent_coalesced"},
		{table: "encrypted_secrets", column: "credential_identity"},
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2 AND column_name=$3)`, schema, check.table, check.column).Scan(&exists); err != nil || exists != want {
			t.Fatalf("column %s.%s exists=%t err=%v, want %t", check.table, check.column, exists, err, want)
		}
	}
	for _, trigger := range []string{
		"inventory_source_configurations_retention",
		"library_sources_configured_inventory_retention",
		"encrypted_secrets_configured_inventory_retention",
		"inventory_source_configuration_audit_append_only",
		"inventory_manual_sync_intents_append_only",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = ANY(ARRAY[$1::regclass,$2::regclass,$3::regclass,$4::regclass,$5::regclass]) AND tgname=$6 AND NOT tgisinternal)`, schema+".inventory_source_configurations", schema+".library_sources", schema+".encrypted_secrets", schema+".inventory_source_configuration_audit", schema+".inventory_manual_sync_intents", trigger).Scan(&exists); err != nil || exists != want {
			t.Fatalf("trigger %s exists=%t err=%v, want %t", trigger, exists, err, want)
		}
	}
	var constraintExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid=$1::regclass AND conname='inventory_source_configuration_audit_manual_intent_outcome_check')`, schema+".inventory_source_configuration_audit").Scan(&constraintExists); err != nil || constraintExists != want {
		t.Fatalf("manual intent outcome constraint exists=%t err=%v, want %t", constraintExists, err, want)
	}
}

func assertTableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema, table string, want bool) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil || exists != want {
		t.Fatalf("table %s exists=%t err=%v, want %t", table, exists, err, want)
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
		{name: "ahead", version: 9, wantError: "migration history version is newer than this binary"},
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
