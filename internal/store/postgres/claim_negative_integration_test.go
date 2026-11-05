//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"
)

// claimFixture creates a user with one identity and arms setup_state with a
// token for that identity. Every negative case below must leave both the
// setup state and the roles untouched.
type claimFixture struct {
	ctx            context.Context
	store          *Store
	issuer         string
	subject        string
	userID         string
	otherUserID    string
	otherSubject   string
	token          string
	tokenHash      [32]byte
	expiresInMinus bool
}

func newClaimFixture(t *testing.T, tokenLifetime string) *claimFixture {
	t.Helper()
	url := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	store, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	f := &claimFixture{ctx: ctx, store: store, issuer: "https://issuer.test", token: "negative-token-" + newUUID()}
	f.subject = "claim-target-" + newUUID()
	f.otherSubject = "claim-other-" + newUUID()
	f.tokenHash = sha256.Sum256([]byte(f.token))
	mk := func(subject string) string {
		var id string
		if err := store.pool.QueryRow(ctx, `INSERT INTO users(id,oidc_subject,email,display_name,role,created_at,updated_at) VALUES($1,$2,'','claim test','viewer',now(),now()) RETURNING id::text`, newUUID(), "claim-test-"+newUUID()).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := store.pool.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id) VALUES($1,$2,$3)`, f.issuer, subject, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.userID, f.otherUserID = mk(f.subject), mk(f.otherSubject)
	reset := `UPDATE setup_state SET complete=FALSE,target_issuer='',target_subject='',token_hash=NULL,token_expires_at=NULL,claimed_by=NULL,completed_at=NULL WHERE singleton=TRUE`
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), reset)
		for _, id := range []string{f.userID, f.otherUserID} {
			_, _ = store.pool.Exec(context.Background(), `DELETE FROM oidc_identities WHERE user_id=$1`, id)
			_, _ = store.pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, id)
		}
	})
	if _, err := store.pool.Exec(ctx, reset); err != nil {
		t.Fatal(err)
	}
	if err := store.IssueAdminToken(ctx, f.issuer, f.subject, f.token, time.Now().Add(30*time.Minute), false); err != nil {
		t.Fatal(err)
	}
	if tokenLifetime == "expired" {
		if _, err := store.pool.Exec(ctx, `UPDATE setup_state SET token_expires_at=now()-interval '1 minute' WHERE singleton=TRUE`); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *claimFixture) assertUnchanged(t *testing.T) {
	t.Helper()
	var admins int
	if err := f.store.pool.QueryRow(f.ctx, `SELECT count(*) FROM users WHERE id=ANY($1) AND role='admin'`, []string{f.userID, f.otherUserID}).Scan(&admins); err != nil || admins != 0 {
		t.Fatalf("a failed claim promoted someone: admins=%d err=%v", admins, err)
	}
	var complete bool
	var claimedBy *string
	if err := f.store.pool.QueryRow(f.ctx, `SELECT complete,claimed_by::text FROM setup_state WHERE singleton=TRUE`).Scan(&complete, &claimedBy); err != nil || complete || claimedBy != nil {
		t.Fatalf("a failed claim changed setup state: complete=%v claimedBy=%v err=%v", complete, claimedBy, err)
	}
	var claimEvents int
	if err := f.store.pool.QueryRow(f.ctx, `SELECT count(*) FROM setup_audit_events WHERE event_type='admin_claimed' AND target_subject=ANY($1)`, []string{f.subject, f.otherSubject}).Scan(&claimEvents); err != nil || claimEvents != 0 {
		t.Fatalf("a failed claim wrote an admin_claimed audit event: %d %v", claimEvents, err)
	}
}

func TestClaimNegativesLeaveEverythingUnchangedIntegration(t *testing.T) {
	t.Run("wrong token", func(t *testing.T) {
		f := newClaimFixture(t, "")
		wrong := sha256.Sum256([]byte("not the token"))
		if err := f.store.ClaimAdmin(f.ctx, f.userID, f.issuer, f.subject, wrong[:], time.Now()); err == nil {
			t.Fatal("wrong token claimed admin")
		}
		f.assertUnchanged(t)
	})
	t.Run("expired token", func(t *testing.T) {
		f := newClaimFixture(t, "expired")
		if err := f.store.ClaimAdmin(f.ctx, f.userID, f.issuer, f.subject, f.tokenHash[:], time.Now()); err == nil {
			t.Fatal("expired token claimed admin")
		}
		f.assertUnchanged(t)
	})
	t.Run("token issued for a different subject", func(t *testing.T) {
		f := newClaimFixture(t, "")
		if err := f.store.ClaimAdmin(f.ctx, f.otherUserID, f.issuer, f.otherSubject, f.tokenHash[:], time.Now()); err == nil {
			t.Fatal("a token for another subject claimed admin")
		}
		f.assertUnchanged(t)
	})
	t.Run("different signed-in user claims with the target's identity", func(t *testing.T) {
		f := newClaimFixture(t, "")
		if err := f.store.ClaimAdmin(f.ctx, f.otherUserID, f.issuer, f.subject, f.tokenHash[:], time.Now()); err == nil {
			t.Fatal("another user claimed admin with the target's identity")
		}
		f.assertUnchanged(t)
	})
	t.Run("different issuer for the same subject", func(t *testing.T) {
		f := newClaimFixture(t, "")
		if err := f.store.ClaimAdmin(f.ctx, f.userID, "https://evil.test", f.subject, f.tokenHash[:], time.Now()); err == nil {
			t.Fatal("a different issuer claimed admin")
		}
		f.assertUnchanged(t)
	})
	t.Run("second claim after success", func(t *testing.T) {
		f := newClaimFixture(t, "")
		if err := f.store.ClaimAdmin(f.ctx, f.userID, f.issuer, f.subject, f.tokenHash[:], time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := f.store.ClaimAdmin(f.ctx, f.userID, f.issuer, f.subject, f.tokenHash[:], time.Now()); err == nil {
			t.Fatal("the token was usable twice")
		}
		if err := f.store.ClaimAdmin(f.ctx, f.otherUserID, f.issuer, f.otherSubject, f.tokenHash[:], time.Now()); err == nil {
			t.Fatal("another user used the spent token")
		}
		var role string
		if err := f.store.pool.QueryRow(f.ctx, `SELECT role FROM users WHERE id=$1`, f.otherUserID).Scan(&role); err != nil || role != "viewer" {
			t.Fatalf("other user role=%q err=%v", role, err)
		}
		var events int
		if err := f.store.pool.QueryRow(f.ctx, `SELECT count(*) FROM setup_audit_events WHERE event_type='admin_claimed' AND target_subject=$1`, f.subject).Scan(&events); err != nil || events != 1 {
			t.Fatalf("admin_claimed events=%d err=%v", events, err)
		}
	})
}

func TestSetupCompleteIsFalseUntilAClaimSucceedsIntegration(t *testing.T) {
	f := newClaimFixture(t, "")
	if done, err := f.store.SetupComplete(f.ctx); err != nil || done {
		t.Fatalf("before claim: done=%v err=%v", done, err)
	}
	if err := f.store.ClaimAdmin(f.ctx, f.userID, f.issuer, f.subject, f.tokenHash[:], time.Now()); err != nil {
		t.Fatal(err)
	}
	if done, err := f.store.SetupComplete(f.ctx); err != nil || !done {
		t.Fatalf("after claim: done=%v err=%v", done, err)
	}
}
