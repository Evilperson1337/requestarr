//go:build integration

package migrations

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	embedded "git.example.com/evilperson1337/Requestarr/db/migrations"
)

func TestAudiobookMigrationsUpgradeFromExactV6AndSafeRecovery(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyThroughV6(t, ctx, pool)
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES (6, false)`); err != nil {
		t.Fatalf("seed v6 history: %v", err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("upgrade v6 through v8: %v", err)
	}
	assertAudiobookSchema(t, ctx, pool, schema)
	seedEvidenceReferences(t, ctx, pool)
	insertPossibleEvidence(t, ctx, pool, "00000000-0000-4000-8000-000000000005")
	if _, err := pool.Exec(ctx, `INSERT INTO reconciliation_match_events(id,evidence_id,event_type,created_at) VALUES ('00000000-0000-4000-8000-000000000008','00000000-0000-4000-8000-000000000005','activated',clock_timestamp())`); err != nil {
		t.Fatalf("insert reconciliation audit event: %v", err)
	}
	downV9, err := fs.ReadFile(embedded.Files, "000009_reconciliation_audit.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(downV9)); err == nil {
		t.Fatal("v9 rollback silently removed reconciliation audit history")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM reconciliation_match_events`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(downV9)); err != nil {
		t.Fatalf("safe empty v9 rollback: %v", err)
	}
	for _, name := range []string{"000008_reconciliation_match_evidence.down.sql", "000007_audiobook_catalog_sources.down.sql"} {
		down, err := fs.ReadFile(embedded.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(down)); err == nil {
			t.Fatalf("%s silently deleted persisted history", name)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM reconciliation_match_evidence; DELETE FROM inventory_items; DELETE FROM requests; DELETE FROM catalog_items; DELETE FROM library_sources`); err != nil {
		t.Fatalf("clear local rollback data: %v", err)
	}
	for _, name := range []string{"000008_reconciliation_match_evidence.down.sql", "000007_audiobook_catalog_sources.down.sql"} {
		down, _ := fs.ReadFile(embedded.Files, name)
		if _, err := pool.Exec(ctx, string(down)); err != nil {
			t.Fatalf("safe empty rollback %s: %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=6, dirty=false`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v7/v8: %v", err)
	}
}

func TestAudiobookInventorySnapshotMigrationUpgradeRollbackAndReapply(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyThroughV9(t, ctx, pool)
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations(version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL); INSERT INTO schema_migrations VALUES (9, false)`); err != nil {
		t.Fatalf("seed v9 history: %v", err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("upgrade exact v9 through v10: %v", err)
	}
	for _, relation := range []string{"audiobook_source_libraries", "audiobook_inventory_snapshots", "audiobook_inventory_details", "inventory_items_source_live_snapshot_idx"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+relation).Scan(&exists); err != nil || !exists {
			t.Fatalf("expected v10 relation %s: exists=%t err=%v", relation, exists, err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,authoritative_media_types,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000010','audiobookshelf','Books','https://books.example','["audiobook"]',clock_timestamp(),clock_timestamp()); INSERT INTO audiobook_inventory_snapshots(id,source_id,content_hash,completed_at,committed_at) VALUES ('00000000-0000-4000-8000-000000000011','00000000-0000-4000-8000-000000000010',repeat('a',64),clock_timestamp(),clock_timestamp())`); err != nil {
		t.Fatalf("seed v10 history: %v", err)
	}
	down, err := fs.ReadFile(embedded.Files, "000010_audiobookshelf_inventory_snapshots.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("v10 rollback silently removed persisted snapshot history")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM audiobook_inventory_snapshots; DELETE FROM library_sources WHERE id='00000000-0000-4000-8000-000000000010'`); err != nil {
		t.Fatalf("clear v10 history: %v", err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("safe empty v10 rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET version=9, dirty=false`); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply v10: %v", err)
	}
}

func TestAudiobookshelfSourceConfigurationMigrationGuardedRollback(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	down, err := fs.ReadFile(embedded.Files, "000014_audiobookshelf_source_configuration.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,enabled,authoritative_media_types,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000241','audiobookshelf','Books',NULL,true,'["audiobook"]',clock_timestamp(),clock_timestamp()); INSERT INTO inventory_source_configurations(source_id,endpoint_url,updated_at) VALUES ('00000000-0000-4000-8000-000000000241','https://books.example.test',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err == nil {
		t.Fatal("v14 rollback silently removed source configuration")
	}
}

func TestSourceConfigurationProjectionMigrationRollbackReapplyAudiobookshelfOnly(t *testing.T) {
	databaseURL, _, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"000020_source_configuration_credential_envelope_evidence.down.sql",
		"000019_source_configuration_projection_integrity.down.sql",
		"000019_source_configuration_projection_integrity.up.sql",
		"000020_source_configuration_credential_envelope_evidence.up.sql",
	} {
		migration, err := fs.ReadFile(embedded.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
}

func TestReconciliationEvidenceConstraintsAndActiveLifecycle(t *testing.T) {
	databaseURL, schema, pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	assertAudiobookSchema(t, ctx, pool, schema)
	seedEvidenceReferences(t, ctx, pool)
	insertPossibleEvidence(t, ctx, pool, "00000000-0000-4000-8000-000000000005")
	assertConstraintFailure(t, ctx, pool, `INSERT INTO reconciliation_match_evidence(id,request_id,source_id,inventory_item_id,method,confidence,reason,evidence,review_state,active,observed_at,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000006','00000000-0000-4000-8000-000000000003','00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000004','asin_region','possible','bounded','[]','pending',TRUE,clock_timestamp(),clock_timestamp(),clock_timestamp())`)
	if _, err := pool.Exec(ctx, `UPDATE reconciliation_match_evidence SET active=FALSE, deactivated_at=clock_timestamp() WHERE id='00000000-0000-4000-8000-000000000005'`); err != nil {
		t.Fatalf("deactivate active association: %v", err)
	}
	insertPossibleEvidence(t, ctx, pool, "00000000-0000-4000-8000-000000000006")
	assertConstraintFailure(t, ctx, pool, `INSERT INTO reconciliation_match_evidence(id,request_id,source_id,inventory_item_id,method,confidence,reason,evidence,review_state,active,observed_at,created_at,updated_at,deactivated_at) VALUES ('00000000-0000-4000-8000-000000000007','00000000-0000-4000-8000-000000000003','00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000004','manual','high','','[]','pending',FALSE,clock_timestamp(),clock_timestamp(),clock_timestamp(),clock_timestamp())`)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO reconciliation_match_evidence(id,request_id,source_id,inventory_item_id,method,confidence,reason,evidence,review_state,active,observed_at,created_at,updated_at,deactivated_at) VALUES ('00000000-0000-4000-8000-000000000007','00000000-0000-4000-8000-000000000003','00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000004','manual','possible','','{}','pending',FALSE,clock_timestamp(),clock_timestamp(),clock_timestamp(),clock_timestamp())`)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO reconciliation_match_evidence(id,request_id,source_id,inventory_item_id,method,confidence,reason,evidence,review_state,active,observed_at,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000007','00000000-0000-4000-8000-000000000003','00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000004','manual','possible','',jsonb_build_array(jsonb_build_object('key','k','value',repeat('x',257))),'pending',TRUE,clock_timestamp(),clock_timestamp(),clock_timestamp())`)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO reconciliation_match_evidence(id,request_id,source_id,inventory_item_id,method,confidence,reason,evidence,review_state,active,observed_at,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000007','00000000-0000-4000-8000-000000000003','00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000004','manual','possible','','[{"key":"k","value":"v","raw":"forbidden"}]','pending',TRUE,clock_timestamp(),clock_timestamp(),clock_timestamp())`)
	assertConstraintFailure(t, ctx, pool, `INSERT INTO reconciliation_match_evidence(id,request_id,source_id,inventory_item_id,method,confidence,reason,evidence,review_state,active,observed_at,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000007','00000000-0000-4000-8000-000000000003','00000000-0000-4000-8000-000000000002','00000000-0000-4000-8000-000000000004','manual','possible','','[]','pending',TRUE,clock_timestamp(),clock_timestamp(),clock_timestamp())`)
}

func insertPossibleEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO reconciliation_match_evidence(id,request_id,source_id,inventory_item_id,method,confidence,reason,evidence,review_state,active,observed_at,created_at,updated_at) VALUES ($1,'00000000-0000-4000-8000-000000000003','00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000004','asin_region','possible','bounded','[{"key":"asin","value":"B012345678"}]','pending',TRUE,clock_timestamp(),clock_timestamp(),clock_timestamp())`, id); err != nil {
		t.Fatalf("insert evidence: %v", err)
	}
}

func seedEvidenceReferences(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `INSERT INTO users(id,oidc_subject,display_name,role,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000001','evidence-test','Test','admin',$1,$1); INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000002','audiobook','audible','B012345678-US','Test','square',$1,$1); INSERT INTO requests(id,catalog_item_id,status,requested_by,requested_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000003','00000000-0000-4000-8000-000000000002','requested','00000000-0000-4000-8000-000000000001',$1,$1); INSERT INTO library_sources(id,kind,name,base_url,authoritative_media_types,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000001','audiobookshelf','Books','https://books.example','["audiobook"]',$1,$1); INSERT INTO inventory_items(id,source_id,source_item_id,media_type,title,first_seen_at,last_seen_at) VALUES ('00000000-0000-4000-8000-000000000004','00000000-0000-4000-8000-000000000001','book-1','audiobook','Test',$1,$1)`, now)
	if err != nil {
		t.Fatalf("seed evidence references: %v", err)
	}
}

func assertAudiobookSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string) {
	t.Helper()
	for _, index := range []string{"reconciliation_match_evidence_active_association_key", "reconciliation_match_evidence_request_active_idx", "reconciliation_match_evidence_inventory_active_idx", "reconciliation_match_evidence_possible_review_idx"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+index).Scan(&exists); err != nil || !exists {
			t.Fatalf("expected index %s: exists=%t err=%v", index, exists, err)
		}
	}
	var activeDefinition, reviewDefinition string
	if err := pool.QueryRow(ctx, `SELECT pg_get_indexdef($1::regclass), pg_get_indexdef($2::regclass)`, schema+".reconciliation_match_evidence_active_association_key", schema+".reconciliation_match_evidence_possible_review_idx").Scan(&activeDefinition, &reviewDefinition); err != nil {
		t.Fatalf("read partial index definitions: %v", err)
	}
	if !containsAll(activeDefinition, "UNIQUE", "request_id", "inventory_item_id", "WHERE active") || !containsAll(reviewDefinition, "WHERE", "active", "review_state", "pending") {
		t.Fatalf("unexpected partial index definitions: active=%q review=%q", activeDefinition, reviewDefinition)
	}
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}

func applyThroughV6(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, name := range []string{"000001_initial.up.sql", "000002_setup_bootstrap_sessions.up.sql", "000003_setup_workflow_events.up.sql", "000004_encrypted_secrets.up.sql", "000005_setup_authentication_saved.up.sql", "000006_oidc_discovery_validation.up.sql"} {
		migration, err := fs.ReadFile(embedded.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
}

func applyThroughV9(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, name := range []string{"000001_initial.up.sql", "000002_setup_bootstrap_sessions.up.sql", "000003_setup_workflow_events.up.sql", "000004_encrypted_secrets.up.sql", "000005_setup_authentication_saved.up.sql", "000006_oidc_discovery_validation.up.sql", "000007_audiobook_catalog_sources.up.sql", "000008_reconciliation_match_evidence.up.sql", "000009_reconciliation_audit.up.sql"} {
		migration, err := fs.ReadFile(embedded.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
}
