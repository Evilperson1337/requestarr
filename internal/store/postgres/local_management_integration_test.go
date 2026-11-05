//go:build integration

package postgres

import (
	"errors"
	"github.com/evilperson1337/requestarr/internal/auth"
	"testing"
	"time"
)

func TestAdministratorCanCreateAndSafelyDisableRecoveryAccountIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	actor := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, actor); err != nil {
		t.Fatal("promote actor failed")
	}
	service := auth.AdminManagement{Repo: s, Hasher: fastHasher}
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
