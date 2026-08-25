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
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/reconcile"
	"git.example.com/evilperson1337/Requestarr/internal/store/postgres/migrations"
)

func TestReconciliationEvidenceRepositoryLifecycle(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	seedReconciliationReferences(t, ctx, pool)
	repo := NewReconciliationEvidenceRepository(&Pool{pool: pool})
	activation := testActivation()
	first, err := repo.Activate(ctx, activation)
	if err != nil || !first.Changed {
		t.Fatalf("first activation: change=%+v err=%v", first, err)
	}
	second, err := repo.Activate(ctx, activation)
	if err != nil || second.Changed {
		t.Fatalf("idempotent activation: change=%+v err=%v", second, err)
	}
	deactivated, err := repo.Deactivate(ctx, activation.Target, time.Now().UTC())
	if err != nil || !deactivated.Changed {
		t.Fatalf("deactivate: change=%+v err=%v", deactivated, err)
	}
	if _, err = repo.Deactivate(ctx, activation.Target, time.Now().UTC()); err != nil {
		t.Fatalf("idempotent deactivation: %v", err)
	}
	reactivated, err := repo.Activate(ctx, activation)
	if err != nil || !reactivated.Changed || reactivated.AssociationID == first.AssociationID {
		t.Fatalf("reactivate: change=%+v err=%v", reactivated, err)
	}
	var events, rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM reconciliation_match_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM reconciliation_match_evidence`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if events != 3 || rows != 2 {
		t.Fatalf("events=%d rows=%d", events, rows)
	}
}

func TestReconciliationEvidenceRepositoryConcurrentAndProvenance(t *testing.T) {
	databaseURL, pool := isolatedReconciliationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	seedReconciliationReferences(t, ctx, pool)
	repo := NewReconciliationEvidenceRepository(&Pool{pool: pool})
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := repo.Activate(ctx, testActivation()); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent activate: %v", err)
		}
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM reconciliation_match_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("activation audit events=%d", events)
	}
	bad := testActivation()
	bad.Target.SourceID = "00000000-0000-4000-8000-000000000099"
	if _, err := repo.Activate(ctx, bad); !errors.Is(err, ErrReconciliationProvenance) {
		t.Fatalf("provenance error=%v", err)
	}
	canceled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := repo.Activate(canceled, testActivation()); !errors.Is(err, reconcile.ErrCanceled) {
		t.Fatalf("cancel error=%v", err)
	}
}

func testActivation() reconcile.Activation {
	return reconcile.Activation{Target: reconcile.Target{RequestID: "00000000-0000-4000-8000-000000000003", SourceID: "00000000-0000-4000-8000-000000000001", InventoryItemID: "00000000-0000-4000-8000-000000000004"}, Evidence: domain.MatchEvidence{Method: domain.MatchASINRegion, Confidence: domain.MatchExact, Attributes: []domain.MatchAttribute{{Key: "asin", Value: "B012345678"}}}, ObservedAt: time.Now().UTC()}
}

func seedReconciliationReferences(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Now().UTC()
	_, err := pool.Exec(ctx, `INSERT INTO users(id,oidc_subject,display_name,role,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000001','reconcile-test','Test','admin',$1,$1); INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000002','audiobook','audible','B012345678-US','Test','square',$1,$1); INSERT INTO requests(id,catalog_item_id,status,requested_by,requested_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000003','00000000-0000-4000-8000-000000000002','requested','00000000-0000-4000-8000-000000000001',$1,$1); INSERT INTO library_sources(id,kind,name,base_url,authoritative_media_types,created_at,updated_at) VALUES ('00000000-0000-4000-8000-000000000001','audiobookshelf','Books','https://books.example','["audiobook"]',$1,$1); INSERT INTO inventory_items(id,source_id,source_item_id,media_type,title,first_seen_at,last_seen_at) VALUES ('00000000-0000-4000-8000-000000000004','00000000-0000-4000-8000-000000000001','book-1','audiobook','Test',$1,$1)`, now)
	if err != nil {
		t.Fatalf("seed references: %v", err)
	}
}

var reconciliationSchemaPattern = regexp.MustCompile(`^requestarr_reconcile_[0-9a-f]{32}$`)

func isolatedReconciliationDatabase(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	baseURL := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL is required for integration tests")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("REQUESTARR_TEST_DATABASE_URL must be an absolute PostgreSQL URL")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	schema := "requestarr_reconcile_" + hex.EncodeToString(random)
	if !reconciliationSchemaPattern.MatchString(schema) {
		t.Fatal("unsafe generated schema")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = admin.Exec(cleanupCtx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		admin.Close()
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	pool, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return parsed.String(), pool
}
