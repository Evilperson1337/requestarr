//go:build integration

package postgres

import (
	"context"
	"errors"
	"github.com/evilperson1337/requestarr/internal/auth"
	"sync"
	"testing"
	"time"
)

func TestAdministratorCanCreateAndSafelyDisableRecoveryAccountIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	actor := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, actor); err != nil {
		t.Fatal("promote actor failed")
	}
	service := auth.AdminManagement{Repo: s, Hasher: fastHasher, OIDCWorking: func() bool { return true }}
	st, err := service.LocalAdminStatus(ctx)
	if err != nil || st.Exists {
		t.Fatal("upgrade must not invent local administrator")
	}
	created, err := service.Create(ctx, actor)
	if err != nil || created.Password == "" {
		t.Fatal("explicit recovery creation failed")
	}
	cred, err := s.LocalCredentialByUsername(ctx, "admin")
	if err != nil || !fastHasher.Verify(created.Password, cred.PasswordHash) || !cred.MustChange {
		t.Fatal("generated credential must be hashed and force change")
	}
	again, err := service.Create(ctx, actor)
	if !errors.Is(err, auth.ErrLocalAdminExists) || again.Password != "" {
		t.Fatal("creation must never return another password or reset existing account")
	}
	if err := s.CreateSession(ctx, cred.UserID, []byte("management-token"), []byte("csrf"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal("session setup failed")
	}
	if err := service.SetDisabled(ctx, actor, true); err != nil {
		t.Fatal("another admin permits disable")
	}
	if _, _, _, err := s.GetSession(ctx, []byte("management-token"), time.Now()); err == nil {
		t.Fatal("disable must revoke sessions")
	}
	st, err = service.LocalAdminStatus(ctx)
	if err != nil || !st.Disabled || st.CanDisable {
		t.Fatal("local status wrong")
	}
	// A disabled recovery account does not count as a surviving administrator.
	other := integrationUser(t, ctx, s)
	if err := s.ChangeUserRole(ctx, other, actor, "viewer", time.Now()); !errors.Is(err, ErrLastAdmin) {
		t.Fatal("disabled account must not satisfy last-admin protection")
	}
	if err := service.SetDisabled(ctx, actor, false); err != nil {
		t.Fatal("enable failed")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='viewer' WHERE id=$1`, actor); err != nil {
		t.Fatal("test setup failed")
	}
	if err := service.SetDisabled(ctx, cred.UserID, true); !errors.Is(err, auth.ErrLastLocalAdmin) {
		t.Fatal("last enabled admin must not be disabled")
	}
	if err := s.ChangeUserRole(ctx, other, cred.UserID, "viewer", time.Now()); !errors.Is(err, auth.ErrBuiltinRole) {
		t.Fatal("built-in admin role must be locked")
	}
	// A failed actor audit cannot change the credential state.
	if err := service.SetDisabled(ctx, "ffffffff-ffff-4fff-8fff-ffffffffffff", false); err != nil && !errors.Is(err, auth.ErrAdminManagement) {
		t.Fatal("unsafe storage error")
	}
}

func newManagedInstance(t *testing.T) (context.Context, *Store, string, string) {
	t.Helper()
	ctx, s := freshStore(t)
	other := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, other); err != nil {
		t.Fatal("promote failed")
	}
	hash, err := fastHasher.Hash("a password for the local account")
	if err != nil {
		t.Fatal("hash failed")
	}
	if ok, err := s.CreateRecoveryAdmin(ctx, other, hash, time.Now()); err != nil || !ok {
		t.Fatal("create failed")
	}
	cred, err := s.LocalCredentialByUsername(ctx, "admin")
	if err != nil {
		t.Fatal("credential failed")
	}
	return ctx, s, other, cred.UserID
}

func TestDisablingTheLocalAdministratorNeedsOIDCToBeWorkingIntegration(t *testing.T) {
	ctx, s, other, local := newManagedInstance(t)
	for _, oidc := range []bool{false, true} {
		svc := auth.AdminManagement{Repo: s, OIDCWorking: func() bool { return oidc }}
		err := svc.SetDisabled(ctx, other, true)
		st, serr := svc.LocalAdminStatus(ctx)
		if serr != nil {
			t.Fatal("status failed")
		}
		if oidc {
			if err != nil || !st.Disabled {
				t.Fatalf("OIDC working: err=%v status=%+v", err, st)
			}
			continue
		}
		if !errors.Is(err, auth.ErrNoWorkingSignIn) || st.Disabled || st.CanDisable || !st.OtherAdmin || st.OIDCWorking {
			t.Fatalf("OIDC not working: err=%v status=%+v", err, st)
		}
		if count(t, ctx, s, `SELECT count(*) FROM local_admin_management_audit_events WHERE target_user_id=$1 AND action='disable'`, local) != 0 {
			t.Fatal("a refused disable was audited as done")
		}
	}
}

// The OIDC check must run inside the serialised section, not before it.
func TestTheOIDCGuardRunsWhileTheAdminLockIsHeldIntegration(t *testing.T) {
	ctx, s, other, _ := newManagedInstance(t)
	var heldDuringGuard bool
	err := s.SetLocalAdminDisabled(ctx, other, true, time.Now(), func() bool {
		var got bool
		if err := s.pool.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, adminRoleAdvisoryLockID).Scan(&got); err != nil {
			t.Error("probe failed")
		}
		heldDuringGuard = !got
		return true
	})
	if err != nil || !heldDuringGuard {
		t.Fatalf("err=%v heldDuringGuard=%v", err, heldDuringGuard)
	}
}

// Disabling the local account and demoting admins race for the last way in;
// whatever order the lock gives, an enabled administrator must remain.
func TestConcurrentDisablesCannotRemoveTheLastWayInIntegration(t *testing.T) {
	ctx, s, other, _ := newManagedInstance(t)
	second := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, second); err != nil {
		t.Fatal("promote failed")
	}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		_ = s.SetLocalAdminDisabled(ctx, other, true, time.Now(), func() bool { return true })
	}()
	go func() { defer wg.Done(); _ = s.ChangeUserRole(ctx, second, other, "viewer", time.Now()) }()
	go func() { defer wg.Done(); _ = s.ChangeUserRole(ctx, other, second, "viewer", time.Now()) }()
	wg.Wait()
	if n := count(t, ctx, s, `SELECT count(*) FROM users u WHERE role='admin' AND NOT EXISTS(SELECT 1 FROM local_credentials c WHERE c.user_id=u.id AND c.disabled)`); n < 1 {
		t.Fatal("no enabled administrator left")
	}
}
