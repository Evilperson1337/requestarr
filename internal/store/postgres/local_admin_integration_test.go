//go:build integration

package postgres

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/auth/password"
	"github.com/jackc/pgx/v5"
)

var fastHasher = password.Hasher{Iterations: 1000}

// freshStore opens a store on a brand-new empty database, so "no users" is
// true by construction and the test cannot disturb the shared one.
func freshStore(t *testing.T) (context.Context, *Store) {
	t.Helper()
	base := testDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	name := "rq_local_admin_" + strings.ReplaceAll(newUUID(), "-", "")[:12]
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close(ctx)
		t.Fatalf("operation failed (%T)", err)
	}
	u.Path = "/" + name
	store, err := Open(ctx, u.String())
	if err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	t.Cleanup(func() {
		store.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		admin.Close(context.Background())
	})
	return ctx, store
}

func count(t *testing.T, ctx context.Context, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	return n
}

func TestFreshInstallCreatesOneBuiltInAdministratorIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	hash, _ := fastHasher.Hash("a generated password value")
	created, err := s.CreateLocalAdmin(ctx, hash, time.Now())
	if err != nil || !created {
		t.Fatalf("created=%v errorType=%T", created, err)
	}
	var role, subject string
	var builtin bool
	if err := s.pool.QueryRow(ctx, `SELECT role,oidc_subject,builtin FROM users`).Scan(&role, &subject, &builtin); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	if role != "admin" || subject != "local:admin" || !builtin {
		t.Fatalf("role=%s subject=%s builtin=%v", role, subject, builtin)
	}
	cred, err := s.LocalCredentialByUsername(ctx, "admin")
	if err != nil || !cred.MustChange || cred.Disabled || cred.PasswordHash != hash {
		t.Fatalf("credential read failed=%v must-change=%v disabled=%v", err != nil, cred.MustChange, cred.Disabled)
	}
	if n := count(t, ctx, s, `SELECT count(*) FROM auth_audit_events WHERE event_type='admin_created'`); n != 1 {
		t.Fatalf("creation audit rows=%d", n)
	}
	// A second call, as on every later start, creates nothing.
	created, err = s.CreateLocalAdmin(ctx, hash, time.Now())
	if err != nil || created {
		t.Fatalf("second call created=%v errorType=%T", created, err)
	}
	if n := count(t, ctx, s, `SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("users=%d", n)
	}
}

func TestNoLocalAdministratorIsCreatedWhenUsersExistIntegration(t *testing.T) {
	for _, role := range []string{"viewer", "requester", "admin"} {
		t.Run(role, func(t *testing.T) {
			ctx, s := freshStore(t)
			if _, err := s.pool.Exec(ctx, `INSERT INTO users(id,oidc_subject,display_name,role,created_at,updated_at) VALUES($1,'oidc:existing','Someone',$2,now(),now())`, newUUID(), role); err != nil {
				t.Fatalf("operation failed (%T)", err)
			}
			created, err := s.CreateLocalAdmin(ctx, "pbkdf2-sha256$1$AAAA$AAAA", time.Now())
			if err != nil || created {
				t.Fatalf("created=%v errorType=%T", created, err)
			}
			if count(t, ctx, s, `SELECT count(*) FROM local_credentials`) != 0 || count(t, ctx, s, `SELECT count(*) FROM users`) != 1 {
				t.Fatal("an upgrade added a credential")
			}
		})
	}
}

func TestConcurrentStartsCreateExactlyOneAdministratorIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.CreateLocalAdmin(ctx, "pbkdf2-sha256$1$AAAA$AAAA", time.Now())
			if err != nil {
				t.Errorf("operation failed (%T)", err)
			}
			results <- c
		}()
	}
	wg.Wait()
	close(results)
	n := 0
	for c := range results {
		if c {
			n++
		}
	}
	if n != 1 || count(t, ctx, s, `SELECT count(*) FROM users`) != 1 {
		t.Fatalf("created %d times", n)
	}
}

func TestOnlyOneBuiltInAccountAndUniqueUsernamesIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	if _, err := s.CreateLocalAdmin(ctx, "pbkdf2-sha256$1$AAAA$AAAA", time.Now()); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO users(id,oidc_subject,display_name,role,created_at,updated_at,builtin) VALUES($1,'local:other','x','admin',now(),now(),true)`, newUUID()); err == nil {
		t.Fatal("a second built-in account was accepted")
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO auth_audit_events(event_type) VALUES('made_up')`); err == nil {
		t.Fatal("an unknown audit event type was accepted")
	}
}

func TestBuiltInAdministratorSessionsWorkWithoutAnOIDCIdentityIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	if _, err := s.CreateLocalAdmin(ctx, "pbkdf2-sha256$1$AAAA$AAAA", time.Now()); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	cred, _ := s.LocalCredentialByUsername(ctx, "admin")
	now := time.Now()
	if err := s.CreateSession(ctx, cred.UserID, []byte("tok"), []byte("csrf"), now.Add(time.Hour)); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	u, _, _, err := s.GetSession(ctx, []byte("tok"), now)
	if err != nil || u.Role != "admin" || !u.MustChange || u.ID != cred.UserID {
		t.Fatalf("session user matches=%v must-change=%v errorType=%T", u.ID == cred.UserID, u.MustChange, err)
	}
	// Disabling the credential ends the session's usefulness at once.
	if _, err := s.pool.Exec(ctx, `UPDATE local_credentials SET disabled=TRUE`); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	if _, _, _, err := s.GetSession(ctx, []byte("tok"), now); err == nil {
		t.Fatal("a disabled local account still has a working session")
	}
}

func TestChangeLocalPasswordIsAtomicAndEndsOtherSessionsIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	if _, err := s.CreateLocalAdmin(ctx, "pbkdf2-sha256$1$AAAA$AAAA", time.Now()); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	cred, _ := s.LocalCredentialByUsername(ctx, "admin")
	now := time.Now()
	for _, tok := range []string{"keep", "other1", "other2"} {
		if err := s.CreateSession(ctx, cred.UserID, []byte(tok), []byte("c"), now.Add(time.Hour)); err != nil {
			t.Fatalf("operation failed (%T)", err)
		}
	}
	// A session of someone else must survive.
	other := newUUID()
	if _, err := s.pool.Exec(ctx, `INSERT INTO users(id,oidc_subject,display_name,role,created_at,updated_at) VALUES($1,'oidc:bystander','B','viewer',now(),now())`, other); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	if err := s.CreateSession(ctx, other, []byte("bystander"), []byte("c"), now.Add(time.Hour)); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	if err := s.ChangeLocalPassword(ctx, cred.UserID, "pbkdf2-sha256$1$BBBB$BBBB", []byte("keep"), "203.0.113.7", now); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	after, _ := s.LocalCredentialByUsername(ctx, "admin")
	if after.MustChange || after.PasswordHash != "pbkdf2-sha256$1$BBBB$BBBB" {
		t.Fatalf("updated credential must-change=%v hashMatches=%v", after.MustChange, after.PasswordHash == "pbkdf2-sha256$1$BBBB$BBBB")
	}
	for tok, want := range map[string]bool{"keep": true, "other1": false, "other2": false, "bystander": true} {
		_, _, _, err := s.GetSession(ctx, []byte(tok), now)
		if (err == nil) != want {
			t.Errorf("session %s alive=%v want %v", tok, err == nil, want)
		}
	}
	if err := s.ChangeLocalPassword(ctx, other, "x", nil, "203.0.113.7", now); err == nil {
		t.Fatal("changed the password of a user who has none")
	}
}

func TestAuditRowsAreWrittenIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	if _, err := s.CreateLocalAdmin(ctx, "pbkdf2-sha256$1$AAAA$AAAA", time.Now()); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	cred, _ := s.LocalCredentialByUsername(ctx, "admin")
	for _, e := range []auth.AuditEvent{
		{Type: auth.AuditSignInSuccess, UserID: cred.UserID, Username: "admin", RemoteAddr: "203.0.113.7", At: time.Now()},
		{Type: auth.AuditSignInFailure, RemoteAddr: "203.0.113.8", At: time.Now()},
	} {
		if err := s.RecordAuthEvent(ctx, e); err != nil {
			t.Fatalf("operation failed (%T)", err)
		}
	}
	if n := count(t, ctx, s, `SELECT count(*) FROM auth_audit_events WHERE event_type='signin_failure' AND user_id IS NULL AND username IS NULL AND remote_addr='203.0.113.8'`); n != 1 {
		t.Fatalf("failure row missing or not anonymous, n=%d", n)
	}
	if n := count(t, ctx, s, `SELECT count(*) FROM auth_audit_events WHERE event_type='signin_success' AND user_id=$1 AND username='admin'`, cred.UserID); n != 1 {
		t.Fatalf("success row missing")
	}
}

// The whole path against the real database: bootstrap on a fresh install, sign
// in with the logged password, change it, and prove the password sits in no
// table, no audit row, no response and no other log output.
func TestGeneratedPasswordAppearsOnlyInTheOneLogLineIntegration(t *testing.T) {
	ctx, s := freshStore(t)
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if err := auth.BootstrapLocalAdmin(ctx, s, auth.BootstrapOptions{PublicURL: "https://requests.example", Hasher: fastHasher, Logger: logger}); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	line := strings.TrimSpace(logs.String())
	_, rest, ok := strings.Cut(line, "one_time_password=")
	if !ok || len(strings.Fields(rest)) == 0 || strings.Contains(line, "\n") {
		t.Fatalf("startup log must have one nonempty password attribute")
	}
	pw := strings.Trim(strings.Fields(rest)[0], `"`)

	handlerLogs := &bytes.Buffer{}
	h := &auth.Handler{Repo: s, Local: s, Hasher: fastHasher, Secret: "integration-secret", PublicURL: "https://requests.example", Secure: true, Logger: slog.New(slog.NewTextHandler(handlerLogs, nil))}
	mux := http.NewServeMux()
	h.Register(mux)
	send := func(method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "203.0.113.7:1234"
		r.Header.Set("Origin", "https://requests.example")
		r.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			r.AddCookie(c)
			if c.Name == "requestarr_csrf" {
				r.Header.Set("X-CSRF-Token", c.Value)
			}
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	bad := send("POST", "/auth/local/login", fmt.Sprintf(`{"username":"admin","password":%q}`, pw+"x"), nil)
	if bad.Code != 401 {
		t.Fatalf("bad status=%d", bad.Code)
	}
	good := send("POST", "/auth/local/login", fmt.Sprintf(`{"username":"admin","password":%q}`, pw), nil)
	if good.Code != 200 || !strings.Contains(good.Body.String(), `"mustChangePassword":true`) {
		t.Fatalf("sign-in status=%d bodyBytes=%d", good.Code, good.Body.Len())
	}
	cookies := good.Result().Cookies()
	me := send("GET", "/auth/me", "", cookies)
	if me.Code != 200 || !strings.Contains(me.Body.String(), `"mustChangePassword":true`) {
		t.Fatalf("me status=%d bodyBytes=%d", me.Code, me.Body.Len())
	}
	chg := send("POST", "/auth/local/password", fmt.Sprintf(`{"currentPassword":%q,"newPassword":"my own chosen passphrase"}`, pw), cookies)
	if chg.Code != 204 {
		t.Fatalf("change status=%d bodyBytes=%d", chg.Code, chg.Body.Len())
	}
	me = send("GET", "/auth/me", "", cookies)
	if strings.Contains(me.Body.String(), `"mustChangePassword":true`) {
		t.Fatal("must-change not cleared")
	}
	if again := send("POST", "/auth/local/login", fmt.Sprintf(`{"username":"admin","password":%q}`, pw), nil); again.Code == 200 {
		t.Fatal("the one-time password still works after the change")
	}

	outputs := map[string]string{
		"failed sign-in": bad.Body.String() + fmt.Sprint(bad.Header()),
		"sign-in":        good.Body.String() + fmt.Sprint(good.Header()),
		"current user":   me.Body.String() + fmt.Sprint(me.Header()),
		"change":         chg.Body.String() + fmt.Sprint(chg.Header()),
		"handler log":    handlerLogs.String(),
	}
	for _, table := range []string{"users", "local_credentials", "auth_audit_events", "auth_sessions", "app_settings", "oidc_identities", "setup_state", "setup_audit_events", "admin_user_audit_events"} {
		rows, err := s.pool.Query(ctx, fmt.Sprintf("SELECT t::text FROM %s t", table))
		if err != nil {
			t.Fatalf("operation failed (%T)", err)
		}
		var dump strings.Builder
		for rows.Next() {
			var row string
			_ = rows.Scan(&row)
			dump.WriteString(row + "\n")
		}
		rows.Close()
		outputs["table "+table] = dump.String()
	}
	for name, out := range outputs {
		if strings.Contains(out, pw) {
			t.Errorf("%s contains the one-time password", name)
		}
	}
	for name, out := range outputs {
		if name != "table local_credentials" && strings.Contains(out, "pbkdf2") {
			t.Errorf("%s contains a password hash", name)
		}
	}
	if !strings.Contains(outputs["table local_credentials"], "pbkdf2-sha256$1000$") {
		t.Error("the credential table should hold the encoded hash")
	}
	if n := strings.Count(logs.String(), pw); n != 1 {
		t.Errorf("password appears %d times in the startup log", n)
	}
}
