//go:build integration

package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/db/migrations"
	"github.com/evilperson1337/requestarr/internal/auth"
)

const diagSecret = "diag-test-session-secret"

func diagService(s *Store) *auth.DiagnosticsTokens {
	return &auth.DiagnosticsTokens{Store: s, Secret: diagSecret}
}

func diagUser(t *testing.T, ctx context.Context, s *Store, role string) auth.User {
	t.Helper()
	id := newUUID()
	if _, err := s.pool.Exec(ctx, `INSERT INTO users(id,oidc_subject,display_name,role,created_at,updated_at) VALUES($1,$2,'Diag',$3,now(),now())`, id, "diag:"+id, role); err != nil {
		t.Fatal(err)
	}
	return auth.User{ID: id, Role: role}
}

func TestDiagnosticsTokenMigrationRollsBackAndForwardIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	up, err := migrations.FS.ReadFile("000042_diagnostics_tokens.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrations.FS.ReadFile("000042_diagnostics_tokens.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if count(t, ctx, s, `SELECT count(*) FROM pg_indexes WHERE tablename='diagnostics_tokens' AND indexdef LIKE '%UNIQUE%token_hash%'`) != 1 {
		t.Fatal("token_hash is not unique")
	}
	svc := diagService(s)
	u := diagUser(t, ctx, s, "superuser")
	if _, _, err := svc.Create(ctx, u, "x", 0, ""); err != nil {
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
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('diagnostics_tokens') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("down left the table (%v)", err)
	}
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

func TestSixthActiveTokenIsRefusedEvenWhenCreatedTogetherIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	svc := diagService(s)
	u := diagUser(t, ctx, s, "superuser")
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, limited := 0, 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := svc.Create(ctx, u, fmt.Sprintf("t%d", i), 0, "")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, auth.ErrDiagnosticsTokenLimit):
				limited++
			default:
				t.Errorf("unexpected error %T", err)
			}
		}(i)
	}
	wg.Wait()
	if ok != 5 || limited != 3 {
		t.Fatalf("created %d, refused %d; want 5 and 3", ok, limited)
	}
	// Revoking one makes room; an expired one never counted.
	list, _ := svc.List(ctx, u)
	if err := svc.Revoke(ctx, u, list[0].ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Create(ctx, u, "again", 0, ""); err != nil {
		t.Fatalf("after a revoke: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE diagnostics_tokens SET expires_at=now()+interval '1 second', created_at=now()-interval '1 day' WHERE id=$1`, list[1].ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, _, err := svc.Create(ctx, u, "after expiry", 0, ""); err != nil {
		t.Fatalf("after an expiry: %v", err)
	}
}

func TestLookupFollowsTheCreatorsRoleAndAbilityToSignInIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	svc := diagService(s)
	u := diagUser(t, ctx, s, "superuser")
	value, tok, err := svc.Create(ctx, u, "agent", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	why := func(err error) auth.DiagnosticsRefusalReason {
		var r *auth.DiagnosticsRefusal
		if errors.As(err, &r) {
			return r.Reason
		}
		return ""
	}
	if got, err := svc.Lookup(ctx, value); err != nil || got.ID != tok.ID {
		t.Fatalf("valid token refused: %v", err)
	}
	if _, err := svc.Lookup(ctx, "rqd_"+strings.Repeat("A", 43)); why(err) != auth.DiagnosticsUnknown {
		t.Fatalf("unknown: %v", err)
	}

	// A local credential that is disabled cannot sign in.
	if _, err := s.pool.Exec(ctx, `INSERT INTO local_credentials(user_id,username,password_hash,must_change,disabled) VALUES($1,'someone','pbkdf2-sha256$1$a$b',false,true)`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, value); why(err) != auth.DiagnosticsCreatorNotSuperuser {
		t.Fatalf("disabled creator: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE local_credentials SET disabled=false WHERE user_id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, value); err != nil {
		t.Fatalf("re-enabled creator: %v", err)
	}

	for _, role := range []string{"admin", "requester", "viewer"} {
		if _, err := s.pool.Exec(ctx, `UPDATE users SET role=$2 WHERE id=$1`, u.ID, role); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Lookup(ctx, value); why(err) != auth.DiagnosticsCreatorNotSuperuser {
			t.Fatalf("creator now %s: %v", role, err)
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='superuser' WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}

	// Expired: move the expiry into the past (created_at moves with it for the CHECK).
	if _, err := s.pool.Exec(ctx, `UPDATE diagnostics_tokens SET created_at=now()-interval '2 days', expires_at=now()-interval '1 day' WHERE id=$1`, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, value); why(err) != auth.DiagnosticsExpired {
		t.Fatalf("expired: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE diagnostics_tokens SET expires_at=now()+interval '1 day' WHERE id=$1`, tok.ID); err != nil {
		t.Fatal(err)
	}

	// Revoke is immediate: the very next lookup is refused.
	if err := svc.Revoke(ctx, u, tok.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, value); why(err) != auth.DiagnosticsRevoked {
		t.Fatalf("revoked: %v", err)
	}
	if err := svc.Revoke(ctx, u, tok.ID, ""); !errors.Is(err, auth.ErrDiagnosticsTokenNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
	if err := svc.Revoke(ctx, u, newUUID(), ""); !errors.Is(err, auth.ErrDiagnosticsTokenNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestLastUsedIsWrittenAtMostOncePerMinuteIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	svc := diagService(s)
	u := diagUser(t, ctx, s, "superuser")
	_, tok, _ := svc.Create(ctx, u, "agent", 0, "")
	t0 := time.Now().UTC().Truncate(time.Second)
	read := func() time.Time {
		var at *time.Time
		if err := s.pool.QueryRow(ctx, `SELECT last_used_at FROM diagnostics_tokens WHERE id=$1`, tok.ID).Scan(&at); err != nil || at == nil {
			t.Fatalf("last_used_at: %v", err)
		}
		return at.UTC()
	}
	if err := s.TouchDiagnosticsToken(ctx, tok.ID, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchDiagnosticsToken(ctx, tok.ID, t0.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !read().Equal(t0) {
		t.Fatal("a touch within a minute was written")
	}
	if err := s.TouchDiagnosticsToken(ctx, tok.ID, t0.Add(61*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !read().Equal(t0.Add(61 * time.Second)) {
		t.Fatal("a touch after a minute was not written")
	}
	list, _ := svc.List(ctx, u)
	if list[0].LastUsedAt == nil {
		t.Fatal("list does not show last use")
	}
}

func TestCreateAndRevokeAuditTheActorAndNeverTheValueIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	svc := diagService(s)
	u := diagUser(t, ctx, s, "superuser")
	value, tok, err := svc.Create(ctx, u, "agent", 0, "198.51.100.7")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, u, tok.ID, "198.51.100.7"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, ctx, s, `SELECT count(*) FROM auth_audit_events WHERE event_type='diagnostics_token_created' AND user_id=$1 AND token_id=$2 AND remote_addr='198.51.100.7'`, u.ID, tok.ID); n != 1 {
		t.Fatalf("created rows=%d", n)
	}
	if n := count(t, ctx, s, `SELECT count(*) FROM auth_audit_events WHERE event_type='diagnostics_token_revoked' AND user_id=$1 AND token_id=$2`, u.ID, tok.ID); n != 1 {
		t.Fatalf("revoked rows=%d", n)
	}
	// A refused create or revoke leaves no row.
	before := count(t, ctx, s, `SELECT count(*) FROM auth_audit_events`)
	_ = svc.Revoke(ctx, u, tok.ID, "")
	if count(t, ctx, s, `SELECT count(*) FROM auth_audit_events`) != before {
		t.Fatal("a refused revoke wrote a row")
	}
	_ = value
}

// The plain value is nowhere in the database, and a canary value never reaches
// the log or the audit.
func TestThePlainTokenIsNowhereInTheDatabaseOrLogIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	svc := diagService(s)
	u := diagUser(t, ctx, s, "superuser")
	value, tok, err := svc.Create(ctx, u, "agent", 0, "198.51.100.7")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimPrefix(value, "rqd_")
	_, _ = svc.Lookup(ctx, value)
	_ = svc.Touch(ctx, tok.ID)
	_, _ = svc.List(ctx, u)
	// A refused lookup of the canary, after revoking.
	_ = svc.Revoke(ctx, u, tok.ID, "198.51.100.7")
	_, _ = svc.Lookup(ctx, value)

	var tables []string
	rows, err := s.pool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	for _, tbl := range tables {
		// Each row as text; a row holding the value in any column matches.
		if n := count(t, ctx, s, fmt.Sprintf(`SELECT count(*) FROM %q x WHERE x::text LIKE '%%'||$1||'%%'`, tbl), body); n != 0 {
			t.Fatalf("the plain token is stored in %s", tbl)
		}
	}
	// And not as raw bytes in the hash column (the hash is not the value).
	if n := count(t, ctx, s, `SELECT count(*) FROM diagnostics_tokens WHERE position(convert_to($1,'UTF8') in token_hash)>0`, value); n != 0 {
		t.Fatal("the hash column holds the value")
	}
	if strings.Contains(logs.String(), body) {
		t.Fatal("the token reached the log")
	}
}

// The bearer gate (the code every token request goes through) over the real
// store: demoting or disabling the creator ends the token on the very next
// request. The router above the gate is covered in internal/httpapi, which
// imports this package and so cannot be built here.
func TestDemotingOrDisablingTheCreatorEndsTheBearerTokenOnTheNextRequestIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	h := &auth.Handler{Secret: diagSecret, Diagnostics: diagService(s)}
	u := diagUser(t, ctx, s, "superuser")
	value, _, err := h.Diagnostics.Create(ctx, u, "agent", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	next := func(addr string) *auth.BearerError {
		r := httptest.NewRequest("GET", "/api/v1/admin/logs", nil)
		r.RemoteAddr = addr + ":4000"
		r.Header.Set("Authorization", "Bearer "+value)
		_, berr := h.AuthenticateBearer(r)
		return berr
	}
	if berr := next("192.0.2.1"); berr != nil {
		t.Fatalf("before: %+v", berr)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if berr := next("192.0.2.2"); berr == nil || berr.Status != 401 || berr.Reason != string(auth.DiagnosticsCreatorNotSuperuser) {
		t.Fatalf("demoted creator: %+v", berr)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='superuser' WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if berr := next("192.0.2.3"); berr != nil {
		t.Fatalf("restored creator: %+v", berr)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO local_credentials(user_id,username,password_hash,must_change,disabled) VALUES($1,'someone','pbkdf2-sha256$1$a$b',false,true)`, u.ID); err != nil {
		t.Fatal(err)
	}
	if berr := next("192.0.2.4"); berr == nil || berr.Status != 401 || berr.Reason != string(auth.DiagnosticsCreatorNotSuperuser) {
		t.Fatalf("disabled creator: %+v", berr)
	}
}
