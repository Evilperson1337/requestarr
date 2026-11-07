//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/db/migrations"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
)

// An upgrade starts from a database holding the old roles, not a fresh one:
// roll migration 40 back, fill the table the way rc.5 left it, apply it again.
func TestSuperuserMigrationPromotesExistingAdminsIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	up, err := migrations.FS.ReadFile("000040_superuser_role.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrations.FS.ReadFile("000040_superuser_role.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("down: %v", err)
	}
	ids := map[string]string{}
	for _, row := range []struct {
		name, role string
		builtin    bool
	}{{"local", "admin", true}, {"oidcadmin", "admin", false}, {"requester", "requester", false}, {"viewer", "viewer", false}} {
		id := newUUID()
		ids[row.name] = id
		if _, err := tx.Exec(ctx, `INSERT INTO users(id,oidc_subject,display_name,role,builtin,created_at,updated_at) VALUES($1,$2,$3,$4,$5,now(),now())`, id, "upgrade:"+id, row.name, row.role, row.builtin); err != nil {
			t.Fatal(err)
		}
	}
	// A history entry written under the old roles must keep reading correctly.
	if _, err := tx.Exec(ctx, `INSERT INTO admin_user_audit_events(actor_user_id,target_user_id,previous_role,new_role) VALUES($1,$2,'requester','admin')`, ids["local"], ids["oidcadmin"]); err != nil {
		t.Fatal(err)
	}
	// The old checks refuse the new role, so the widening is what lets it in.
	if _, err := tx.Exec(ctx, `SAVEPOINT old_check`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET role='superuser' WHERE id=$1`, ids["viewer"]); err == nil {
		t.Fatal("the old role check accepted superuser")
	}
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT old_check`); err != nil {
		t.Fatal(err)
	}

	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatalf("up: %v", err)
	}
	for name, want := range map[string]string{"local": "superuser", "oidcadmin": "superuser", "requester": "requester", "viewer": "viewer"} {
		var got string
		if err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, ids[name]).Scan(&got); err != nil || got != want {
			t.Errorf("%s after upgrade = %q (err %v), want %q", name, got, err, want)
		}
	}
	var prev, next, source string
	if err := tx.QueryRow(ctx, `SELECT previous_role,new_role,source FROM admin_user_audit_events WHERE target_user_id=$1`, ids["oidcadmin"]).Scan(&prev, &next, &source); err != nil || prev != "requester" || next != "admin" || source != "manual" {
		t.Errorf("old audit row = %s -> %s from %s (err %v)", prev, next, source, err)
	}
	// The new roles and sources are accepted; an unknown source is not.
	if _, err := tx.Exec(ctx, `INSERT INTO admin_user_audit_events(actor_user_id,target_user_id,previous_role,new_role,source) VALUES($1,$2,'admin','superuser','group_mapping')`, ids["local"], ids["oidcadmin"]); err != nil {
		t.Errorf("audit rejects superuser or group_mapping: %v", err)
	}
	// The database itself keeps the built-in account a superuser.
	if _, err := tx.Exec(ctx, `SAVEPOINT builtin_check`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, ids["local"]); err == nil {
		t.Error("the database let the built-in account stop being a superuser")
	}
	if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT builtin_check`); err != nil {
		t.Fatal(err)
	}

	// And it rolls back: superusers return to admin, history to admin.
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatalf("down again: %v", err)
	}
	var admins int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin' AND id IN ($1,$2)`, ids["local"], ids["oidcadmin"]).Scan(&admins); err != nil || admins != 2 {
		t.Errorf("admins after rollback = %d (err %v), want 2", admins, err)
	}
}

func TestWhoMayChangeWhichRoleIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	person := func(role string) string {
		id := integrationUser(t, ctx, s)
		if _, err := s.pool.Exec(ctx, `UPDATE users SET role=$2 WHERE id=$1`, id, role); err != nil {
			t.Fatal(err)
		}
		return id
	}
	owner, otherOwner, admin, otherAdmin, requester, viewer := person("superuser"), person("superuser"), person("admin"), person("admin"), person("requester"), person("viewer")
	now := time.Now().UTC()
	roleOf := func(id string) string {
		var r string
		if err := s.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, id).Scan(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	refused := func(err error, needs domain.Role) {
		t.Helper()
		var r *domain.RoleChangeRefused
		if !errors.As(err, &r) || r.Needs != needs {
			t.Fatalf("error = %v, want a refusal needing %s", err, needs)
		}
	}

	// An admin moves people between requester and viewer, and no further.
	if err := s.ChangeUserRole(ctx, admin, requester, "viewer", now); err != nil {
		t.Fatalf("admin: requester to viewer: %v", err)
	}
	refused(s.ChangeUserRole(ctx, admin, viewer, "admin", now), domain.RoleSuperuser)
	refused(s.ChangeUserRole(ctx, admin, viewer, "superuser", now), domain.RoleSuperuser)
	refused(s.ChangeUserRole(ctx, admin, otherAdmin, "viewer", now), domain.RoleSuperuser)
	refused(s.ChangeUserRole(ctx, admin, owner, "viewer", now), domain.RoleSuperuser)
	// Requesters and viewers change nobody's role.
	refused(s.ChangeUserRole(ctx, viewer, requester, "viewer", now), domain.RoleAdmin)
	// Nobody changes their own role, the owner included.
	if err := s.ChangeUserRole(ctx, owner, owner, "admin", now); !errors.Is(err, ErrAdminSelfRoleChange) {
		t.Fatalf("self change = %v", err)
	}
	if roleOf(viewer) != "viewer" || roleOf(otherAdmin) != "admin" || roleOf(owner) != "superuser" {
		t.Fatal("a refused change altered a role")
	}
	var audits int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM admin_user_audit_events`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audit rows = %d (err %v), want only the one allowed change", audits, err)
	}

	// A superuser sets anyone to any role; the audit names the roles and the source, and sessions end.
	token, csrf := []byte("t-"+admin), []byte("c-"+admin)
	if err := s.CreateSession(ctx, admin, token, csrf, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ target, to string }{{admin, "superuser"}, {admin, "viewer"}, {viewer, "admin"}, {otherOwner, "requester"}} {
		if err := s.ChangeUserRole(ctx, owner, step.target, step.to, now); err != nil {
			t.Fatalf("superuser: %s to %s: %v", step.target, step.to, err)
		}
		if roleOf(step.target) != step.to {
			t.Fatalf("role after change = %s, want %s", roleOf(step.target), step.to)
		}
	}
	if _, _, _, err := s.GetSession(ctx, token, now); err == nil {
		t.Fatal("a role change left the person's session alive")
	}
	var prev, next, source string
	if err := s.pool.QueryRow(ctx, `SELECT previous_role,new_role,source FROM admin_user_audit_events WHERE actor_user_id=$1 AND target_user_id=$2 AND new_role='superuser'`, owner, admin).Scan(&prev, &next, &source); err != nil || prev != "admin" || source != "manual" {
		t.Fatalf("audit = %s -> %s from %s (err %v)", prev, next, source, err)
	}
}

func TestTheBuiltInAccountStaysASuperuserIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	hash, _ := fastHasher.Hash("a generated password value")
	if created, err := s.CreateLocalAdmin(ctx, hash, time.Now()); err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	var local string
	if err := s.pool.QueryRow(ctx, `SELECT id::text FROM users WHERE builtin`).Scan(&local); err != nil {
		t.Fatal(err)
	}
	owner := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='superuser' WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"viewer", "requester", "admin"} {
		if err := s.ChangeUserRole(ctx, owner, local, role, time.Now()); !errors.Is(err, auth.ErrBuiltinRole) {
			t.Fatalf("even a superuser demoting the built-in account to %s was not refused (%v)", role, err)
		}
	}
}

// Two superusers demote each other at the same moment. Whatever order the lock
// gives, one of them must remain.
func TestLastSuperuserHoldsUnderConcurrentChangesIntegration(t *testing.T) {
	for round := 0; round < 10; round++ {
		ctx, s := freshStore(t)
		a, b := integrationUser(t, ctx, s), integrationUser(t, ctx, s)
		if _, err := s.pool.Exec(ctx, `UPDATE users SET role='superuser' WHERE id IN ($1,$2)`, a, b); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); errs[0] = s.ChangeUserRole(ctx, a, b, "admin", time.Now()) }()
		go func() { defer wg.Done(); errs[1] = s.ChangeUserRole(ctx, b, a, "admin", time.Now()) }()
		wg.Wait()
		var left int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='superuser'`).Scan(&left); err != nil || left != 1 {
			t.Fatalf("round %d: %d superusers left (err %v, results %v)", round, left, err, errs)
		}
	}
}

// A disabled local administrator does not count as a superuser who can sign in.
func TestLastSuperuserCountsOnlyThoseWhoCanSignInIntegration(t *testing.T) {
	ctx, s, other, local := newManagedInstance(t)
	third := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='superuser' WHERE id=$1`, third); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE local_credentials SET disabled=true WHERE user_id=$1`, local); err != nil {
		t.Fatal(err)
	}
	// The disabled local account acts (as a stale caller could); it does not count.
	if err := s.ChangeUserRole(ctx, local, third, "admin", time.Now()); err != nil {
		t.Fatalf("two people can sign in, one may step down: %v", err)
	}
	if err := s.ChangeUserRole(ctx, local, other, "admin", time.Now()); !errors.Is(err, ErrLastSuperuser) {
		t.Fatalf("the last superuser who can sign in was demoted: %v", err)
	}
}

// staleSuperuser is a superuser who cannot sign in (a disabled local account),
// so it is not counted: the only way to reach the last-superuser guard, because
// a signed-in superuser who demotes another is always one of the survivors.
func staleSuperuser(t *testing.T, ctx context.Context, s *Store) string {
	t.Helper()
	id := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='superuser' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO local_credentials(user_id,username,password_hash,disabled) VALUES($1,$2,'x',true)`, id, "stale-"+id[:8]); err != nil {
		t.Fatal(err)
	}
	return id
}
