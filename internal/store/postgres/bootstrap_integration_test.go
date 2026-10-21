//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"os"
	"sync"
	"testing"
	"time"
)

func TestBootstrapClaimIsSingleUseIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL not set")
	}
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var userID string
	const issuer = "https://issuer.test"
	subject := "bootstrap-integration-subject-" + newUUID()
	err = store.pool.QueryRow(ctx, `INSERT INTO users(id,oidc_subject,email,display_name,role,created_at,updated_at) VALUES($1,$2,'','bootstrap test','requester',now(),now()) RETURNING id::text`, newUUID(), "bootstrap-test-"+newUUID()).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id) VALUES($1,$2,$3)`, issuer, subject, userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM oidc_identities WHERE user_id=$1`, userID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	tokenHash := sha256.Sum256([]byte("integration token"))
	_, err = store.pool.Exec(ctx, `UPDATE setup_state SET complete=FALSE,target_issuer=$1,target_subject=$2,token_hash=$3,token_expires_at=now()+interval '5 minutes',claimed_by=NULL,completed_at=NULL WHERE singleton=TRUE`, issuer, subject, tokenHash[:])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `UPDATE setup_state SET complete=FALSE,target_issuer='',target_subject='',token_hash=NULL,token_expires_at=NULL,claimed_by=NULL,completed_at=NULL WHERE singleton=TRUE`)
	})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- store.ClaimAdmin(ctx, userID, issuer, subject, tokenHash[:], time.Now())
		}()
	}
	wg.Wait()
	close(results)
	claimed := 0
	for err := range results {
		if err == nil {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("successful claims=%d, want exactly one", claimed)
	}
	var role string
	if err := store.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, userID).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if role != "admin" {
		t.Fatalf("role=%q", role)
	}
}

func TestBootstrapClaimSerializesWithLastAdminDemotionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL not set")
	}
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.pool.Exec(ctx, `UPDATE users SET role='requester'`); err != nil {
		t.Fatal(err)
	}
	makeIdentity := func(subject string) string {
		t.Helper()
		id := newUUID()
		if _, err := store.pool.Exec(ctx, `INSERT INTO users(id,oidc_subject,email,display_name,role,created_at,updated_at) VALUES($1,$2,'','integration','admin',now(),now())`, id, subject); err != nil {
			t.Fatal(err)
		}
		return id
	}
	adminOne := makeIdentity("concurrent-admin-one-" + newUUID())
	adminTwo := makeIdentity("concurrent-admin-two-" + newUUID())
	target := makeIdentity("concurrent-claim-target-" + newUUID())
	issuer, subject := "https://issuer.test", "concurrent-claim-"+newUUID()
	if _, err := store.pool.Exec(ctx, `UPDATE users SET role='requester' WHERE id=$1`, target); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id) VALUES($1,$2,$3)`, issuer, subject, target); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM oidc_identities WHERE user_id IN ($1,$2,$3)`, adminOne, adminTwo, target)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1,$2,$3)`, adminOne, adminTwo, target)
		_, _ = store.pool.Exec(context.Background(), `UPDATE setup_state SET complete=FALSE,target_issuer='',target_subject='',token_hash=NULL,token_expires_at=NULL,claimed_by=NULL,completed_at=NULL WHERE singleton=TRUE`)
	})
	tokenHash := sha256.Sum256([]byte("race token " + newUUID()))
	if _, err := store.pool.Exec(ctx, `UPDATE setup_state SET complete=FALSE,target_issuer=$1,target_subject=$2,token_hash=$3,token_expires_at=now()+interval '5 minutes',claimed_by=NULL,completed_at=NULL WHERE singleton=TRUE`, issuer, subject, tokenHash[:]); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	type operationResult struct {
		name string
		err  error
	}
	results := make(chan operationResult, 2)
	go func() {
		<-start
		results <- operationResult{name: "claim", err: store.ClaimAdmin(ctx, target, issuer, subject, tokenHash[:], time.Now().UTC())}
	}()
	go func() {
		<-start
		results <- operationResult{name: "demotion", err: store.ChangeUserRole(ctx, adminOne, adminTwo, "requester", time.Now().UTC())}
	}()
	close(start)
	first, second := <-results, <-results
	byName := map[string]error{first.name: first.err, second.name: second.err}
	claimErr, demoteErr := byName["claim"], byName["demotion"]
	// Result arrival order is nondeterministic. Exactly one operation may fail:
	// if demotion wins first, the claim restores an admin; if claim wins first,
	// demotion leaves the newly claimed administrator in place.
	if claimErr != nil {
		t.Fatalf("claim failed: %v", claimErr)
	}
	if demoteErr != nil {
		t.Fatalf("demotion failed with two admins before its serialized count: %v", demoteErr)
	}
	var admins int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin'`).Scan(&admins); err != nil {
		t.Fatal(err)
	}
	if admins != 2 {
		t.Fatalf("administrator count=%d, want 2 after claim and demotion", admins)
	}
	var targetRole, demotedRole string
	if err := store.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, target).Scan(&targetRole); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, adminTwo).Scan(&demotedRole); err != nil {
		t.Fatal(err)
	}
	if targetRole != "admin" || demotedRole != "requester" {
		t.Fatalf("target role=%q, demoted role=%q", targetRole, demotedRole)
	}
}
