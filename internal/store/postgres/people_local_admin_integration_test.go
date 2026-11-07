//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
)

// The local administrator among people (D040): marked in the list, role locked in
// storage, and OIDC people promoted exactly as before.
func TestLocalAdministratorAmongPeopleIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	hash, _ := fastHasher.Hash("a generated password value")
	if created, err := s.CreateLocalAdmin(ctx, hash, time.Now()); err != nil || !created {
		t.Fatalf("created=%v errorType=%T", created, err)
	}
	var local string
	if err := s.pool.QueryRow(ctx, `SELECT id::text FROM users WHERE builtin`).Scan(&local); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	person := integrationUser(t, ctx, s)

	users, err := s.ListAdminUsers(ctx)
	if err != nil || len(users) != 2 {
		t.Fatalf("list failed=%v rows=%d", err != nil, len(users))
	}
	for _, u := range users {
		switch u.ID {
		case local:
			if !u.Builtin || u.Role != "superuser" || u.DisplayName == "" {
				t.Fatalf("local administrator row: builtin=%v role=%s nameSet=%v", u.Builtin, u.Role, u.DisplayName != "")
			}
		case person:
			if u.Builtin {
				t.Fatal("an ordinary person must not be marked builtin")
			}
		}
	}

	// A person is promoted exactly as before, then can be demoted again while the local administrator remains.
	if err := s.ChangeUserRole(ctx, local, person, "admin", time.Now()); err != nil {
		t.Fatalf("promote failed (%T)", err)
	}
	if err := s.ChangeUserRole(ctx, local, person, "viewer", time.Now()); err != nil {
		t.Fatalf("demote failed (%T)", err)
	}

	// Even with another administrator present, the local administrator's role cannot change.
	if err := s.ChangeUserRole(ctx, local, person, "admin", time.Now()); err != nil {
		t.Fatalf("promote failed (%T)", err)
	}
	for _, role := range []string{"viewer", "requester"} {
		if err := s.ChangeUserRole(ctx, person, local, role, time.Now()); !errors.Is(err, auth.ErrBuiltinRole) {
			t.Fatalf("demoting the local administrator to %s was not refused (%T)", role, err)
		}
	}
	var got string
	if err := s.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, local).Scan(&got); err != nil || got != "superuser" {
		t.Fatalf("role changed to %q", got)
	}
}
