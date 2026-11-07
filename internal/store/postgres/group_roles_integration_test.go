//go:build integration

package postgres

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/domain"
)

func TestApplyGroupRoleChangesTheRoleEndsSessionsAndAuditsItIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	person := integrationUser(t, ctx, s)
	if err := s.CreateSession(ctx, person, []byte("token-hash"), []byte("csrf-hash"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyGroupRole(ctx, person, domain.RoleAdmin, time.Now()); err != nil {
		t.Fatal(err)
	}
	var role, prev, source string
	var actor, target string
	if err := s.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, person).Scan(&role); err != nil || role != "admin" {
		t.Fatalf("role = %q (%v)", role, err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT actor_user_id::text,target_user_id::text,previous_role,source FROM admin_user_audit_events WHERE target_user_id=$1`, person).Scan(&actor, &target, &prev, &source); err != nil {
		t.Fatal(err)
	}
	if source != "group_mapping" || actor != person || prev == "admin" {
		t.Fatalf("audit: actor=%s source=%s previous=%s", actor, source, prev)
	}
	var open int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM auth_sessions WHERE user_id=$1 AND revoked_at IS NULL`, person).Scan(&open); err != nil || open != 0 {
		t.Fatalf("%d sessions still open (%v)", open, err)
	}
	// The same role again changes and records nothing.
	if err := s.ApplyGroupRole(ctx, person, domain.RoleAdmin, time.Now()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM admin_user_audit_events WHERE target_user_id=$1`, person).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d audit rows (%v)", n, err)
	}
}

func TestApplyGroupRoleNeverRemovesTheLastSuperuserIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	only := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='superuser' WHERE id=$1`, only); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyGroupRole(ctx, only, domain.RoleViewer, time.Now()); !errors.Is(err, auth.ErrLastSuperuser) {
		t.Fatalf("got %v", err)
	}
	var role string
	if err := s.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, only).Scan(&role); err != nil || role != "superuser" {
		t.Fatalf("role = %q (%v)", role, err)
	}
	// With a second superuser who can sign in, stepping down is allowed.
	second := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='superuser' WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyGroupRole(ctx, only, domain.RoleViewer, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestApplyGroupRoleLeavesTheBuiltInAccountAloneIntegration(t *testing.T) {
	ctx, s, _, local := newManagedInstance(t)
	if err := s.ApplyGroupRole(ctx, local, domain.RoleViewer, time.Now()); err != nil {
		t.Fatal(err)
	}
	var role string
	if err := s.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, local).Scan(&role); err != nil || role != "superuser" {
		t.Fatalf("role = %q (%v)", role, err)
	}
}

type canaryProvider struct{ groups []string }

func (canaryProvider) AuthorizationURL(state, _, _ string) string {
	return "https://id.example/?s=" + state
}
func (p canaryProvider) Exchange(context.Context, string, string, string) (*oidc.Identity, error) {
	return &oidc.Identity{Subject: "group-person", Email: "g@example.test", DisplayName: "G", Groups: p.groups}, nil
}

// The groups decide the role and nothing else: after a sign-in no table holds
// a group name, and neither does the log.
func TestGroupNamesNeverReachTheDatabaseOrTheLogIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	const canary = "canary-group-7f3a"
	mapping := domain.DefaultGroupMapping()
	mapping.Enabled = true
	snap := &authsettings.Snapshot{Provider: canaryProvider{groups: []string{canary, "admin"}}, Issuer: "https://id.example", Groups: mapping}
	var logs strings.Builder
	h := &auth.Handler{Repo: s, GroupRoles: s, OIDC: func() *authsettings.Snapshot { return snap }, Secret: "secret", PublicURL: "https://app.example", Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	mux := http.NewServeMux()
	h.Register(mux)
	login := httptest.NewRecorder()
	mux.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	req := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
	for _, c := range login.Result().Cookies() {
		req.AddCookie(c)
		if c.Name == "requestarr_oidc_state" {
			q := req.URL.Query()
			q.Set("state", c.Value)
			q.Set("code", "c")
			req.URL.RawQuery = q.Encode()
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("callback = %d %s", rec.Code, rec.Body.String())
	}
	var role string
	if err := s.pool.QueryRow(ctx, `SELECT u.role FROM users u JOIN oidc_identities i ON i.user_id=u.id WHERE i.subject='group-person'`).Scan(&role); err != nil || role != "admin" {
		t.Fatalf("the person's role = %q (%v), want admin", role, err)
	}
	rows, err := s.pool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	for _, name := range tables {
		var hits int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM "`+name+`" t WHERE t::text ILIKE '%canary-group%'`).Scan(&hits); err != nil || hits != 0 {
			t.Errorf("table %s holds the canary group name (%d rows, %v)", name, hits, err)
		}
	}
	if strings.Contains(logs.String(), canary) {
		t.Fatalf("the log holds the canary group name: %s", logs.String())
	}
}

// The Users list says whose role was last set by their groups; a later manual
// change takes the mark away.
func TestUsersListMarksRolesSetByGroupsIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	boss := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='superuser' WHERE id=$1`, boss); err != nil {
		t.Fatal(err)
	}
	person := integrationUser(t, ctx, s)
	marked := func() bool {
		list, err := s.ListAdminUsers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range list {
			if u.ID == person {
				return u.RoleFromGroups
			}
		}
		t.Fatal("person not listed")
		return false
	}
	if marked() {
		t.Fatal("a person with no role change is not marked")
	}
	if err := s.ApplyGroupRole(ctx, person, domain.RoleAdmin, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !marked() {
		t.Fatal("a role set by groups is not marked")
	}
	if err := s.ChangeUserRole(ctx, boss, person, "viewer", time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if marked() {
		t.Fatal("a later manual change must clear the mark")
	}
}
