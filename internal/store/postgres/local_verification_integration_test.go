//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
)

func TestLocalSessionRejectsStaleCredentialIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	now := time.Now().UTC()
	oldHash, _ := fastHasher.Hash("initial test passphrase")
	if created, err := s.CreateLocalAdmin(ctx, oldHash, now); err != nil || !created {
		t.Fatal("could not create administrator")
	}
	cred, err := s.LocalCredentialByUsername(ctx, "admin")
	if err != nil {
		t.Fatal("could not read credential")
	}
	if err := s.CreateLocalSession(ctx, cred, []byte("before"), []byte("csrf"), now.Add(time.Hour), "203.0.113.7", now); err != nil {
		t.Fatal("current credential was refused")
	}
	newHash, _ := fastHasher.Hash("replacement test passphrase")
	if err := s.ChangeLocalPassword(ctx, cred.UserID, newHash, []byte("not-kept"), "203.0.113.7", now); err != nil {
		t.Fatal("could not change password")
	}
	if err := s.CreateLocalSession(ctx, cred, []byte("after"), []byte("csrf"), now.Add(time.Hour), "203.0.113.7", now); !errors.Is(err, auth.ErrNoCredential) {
		t.Error("stale credential was not rejected")
	}
	if _, _, _, err := s.GetSession(ctx, []byte("before"), now); err == nil {
		t.Error("pre-change session survived")
	}
	if _, _, _, err := s.GetSession(ctx, []byte("after"), now); err == nil {
		t.Error("stale credential created a live session")
	}
}

func TestPasswordChangeAuditRollsBackAndRecordsExactlyOnceIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	now := time.Now().UTC()
	oldHash, _ := fastHasher.Hash("initial test passphrase")
	if created, err := s.CreateLocalAdmin(ctx, oldHash, now); err != nil || !created {
		t.Fatal("could not create administrator")
	}
	cred, err := s.LocalCredentialByUsername(ctx, "admin")
	if err != nil {
		t.Fatal("could not read credential")
	}
	for _, token := range []string{"keep", "other"} {
		if err := s.CreateLocalSession(ctx, cred, []byte(token), []byte("csrf"), now.Add(time.Hour), "203.0.113.7", now); err != nil {
			t.Fatal("could not create session")
		}
	}
	// Inject a database failure in the actual audit write, after the credential
	// update and revocations have executed. The database is private to this test.
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_password_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='password_changed' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_password_audit BEFORE INSERT ON auth_audit_events FOR EACH ROW EXECUTE FUNCTION reject_password_audit()`); err != nil {
		t.Fatal("could not install audit failure injection")
	}
	newHash, _ := fastHasher.Hash("replacement test passphrase")
	if err := s.ChangeLocalPassword(ctx, cred.UserID, newHash, []byte("keep"), "198.51.100.19", now); err == nil {
		t.Error("password change succeeded despite audit insertion failure")
	}
	after, err := s.LocalCredentialByUserID(ctx, cred.UserID)
	if err != nil || after.PasswordHash != oldHash || !after.MustChange {
		t.Error("audit failure did not roll back credential and must-change")
	}
	for _, token := range []string{"keep", "other"} {
		if _, _, _, err := s.GetSession(ctx, []byte(token), now); err != nil {
			t.Error("audit failure did not roll back session revocations")
		}
	}
	if n := count(t, ctx, s, `SELECT count(*) FROM auth_audit_events WHERE event_type='password_changed'`); n != 0 {
		t.Errorf("failed change audit count=%d, want 0", n)
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER reject_password_audit ON auth_audit_events`); err != nil {
		t.Fatal("could not remove audit failure injection")
	}
	if err := s.ChangeLocalPassword(ctx, cred.UserID, newHash, []byte("keep"), "198.51.100.19", now); err != nil {
		t.Fatal("password change failed after restoring audit")
	}
	after, err = s.LocalCredentialByUserID(ctx, cred.UserID)
	if err != nil || after.PasswordHash != newHash || after.MustChange {
		t.Error("successful change did not update credential")
	}
	if _, _, _, err := s.GetSession(ctx, []byte("keep"), now); err != nil {
		t.Error("successful change revoked current session")
	}
	if _, _, _, err := s.GetSession(ctx, []byte("other"), now); err == nil {
		t.Error("successful change retained other session")
	}
	if n := count(t, ctx, s, `SELECT count(*) FROM auth_audit_events WHERE event_type='password_changed'`); n != 1 {
		t.Errorf("success audit count=%d, want 1", n)
	}
	if n := count(t, ctx, s, `SELECT count(*) FROM auth_audit_events WHERE event_type='password_changed' AND user_id=$1::uuid AND username='admin' AND remote_addr='198.51.100.19' AND created_at=$2`, cred.UserID, now); n != 1 {
		t.Error("password-change audit omitted account, address or time")
	}
}
