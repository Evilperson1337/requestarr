//go:build integration

package postgres

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/auth/password"
)

// Exercise the shipped entry point, then observe recovery through the same
// sign-in/session interfaces as a browser. Never include output in diagnostics.
func TestResetAdminPasswordEntrypointIntegration(t *testing.T) {
	ctx, store := freshStore(t)
	binary := filepath.Join(t.TempDir(), "requestarr")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../../cmd/requestarr")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v (%d output bytes)", err, len(out))
	}
	runWithDatabase := func(databaseURL string, args ...string) (string, string, error) {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, args...)
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "REQUESTARR_") {
				cmd.Env = append(cmd.Env, e)
			}
		}
		cmd.Env = append(cmd.Env, "REQUESTARR_DATABASE_URL="+databaseURL)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		return out.String(), stderr.String(), err
	}
	run := func(args ...string) (string, string, error) {
		return runWithDatabase(store.pool.Config().ConnString(), args...)
	}
	t.Run("database must be explicitly configured", func(t *testing.T) {
		out, stderr, err := runWithDatabase("", "reset-admin-password")
		if err == nil || out != "" || !strings.Contains(stderr, "REQUESTARR_DATABASE_URL") {
			t.Fatal("missing database configuration must fail before connecting with implicit defaults")
		}
	})
	t.Run("unknown commands never start the server", func(t *testing.T) {
		out, stderr, err := run("not-a-command")
		if err == nil || out != "" || !strings.Contains(stderr, "unexpected command") {
			t.Fatal("unknown command must fail before starting the application")
		}
	})
	var out, stderr string
	var err error
	oldPassword := "the old administrator password"
	hash, err := password.Default.Hash(oldPassword)
	if err != nil {
		t.Fatal("hash failed")
	}
	if created, err := store.CreateLocalAdmin(ctx, hash, time.Now()); err != nil || !created {
		t.Fatal("create failed")
	}
	cred, err := store.LocalCredentialByUsername(ctx, "admin")
	if err != nil {
		t.Fatal("credential read failed")
	}
	// Recover a previously chosen password, not an already-forced initial one.
	if err := store.ChangeLocalPassword(ctx, cred.UserID, hash, []byte("none"), "", time.Now()); err != nil {
		t.Fatal("initial password change failed")
	}
	const secret = "entrypoint-test-session-secret"
	for _, token := range []string{"old-session-one", "old-session-two"} {
		if err := store.CreateSession(ctx, cred.UserID, auth.Hash(secret, token), auth.Hash(secret, "csrf"), time.Now().Add(time.Hour)); err != nil {
			t.Fatal("session create failed")
		}
	}
	// A test-private constraint injects an audit failure. Recovery must not
	// publish an unusable password or commit half of the credential/session work.
	t.Run("audit failure rolls back recovery", func(t *testing.T) {
		if _, err := store.pool.Exec(ctx, `ALTER TABLE auth_audit_events ADD CONSTRAINT reject_reset_audit CHECK (event_type <> 'password_changed') NOT VALID`); err != nil {
			t.Fatal("install audit failure fixture failed")
		}
		defer func() {
			if _, err := store.pool.Exec(ctx, `ALTER TABLE auth_audit_events DROP CONSTRAINT reject_reset_audit`); err != nil {
				t.Error("remove audit failure fixture failed")
			}
		}()
		out, stderr, err := run("reset-admin-password")
		if err == nil || out != "" || !strings.Contains(stderr, "could not reset") || strings.Contains(stderr, hash) {
			t.Fatal("failed reset must not print credentials")
		}
		unchanged, err := store.LocalCredentialByUsername(ctx, "admin")
		if err != nil || unchanged.PasswordHash != hash || unchanged.MustChange {
			t.Fatal("failed reset changed the credential")
		}
		for _, token := range []string{"old-session-one", "old-session-two"} {
			if _, _, _, err := store.GetSession(ctx, auth.Hash(secret, token), time.Now()); err != nil {
				t.Fatal("failed reset revoked a session")
			}
		}
	})
	other, err := store.Login(ctx, "https://identity.example", "another-person", "", "Another person", newUUID(), time.Now())
	if err != nil || other.Role != "viewer" {
		t.Fatal("OIDC sign-in must not grant administrator access")
	}
	if err := store.CreateSession(ctx, other.ID, auth.Hash(secret, "other-person-session"), auth.Hash(secret, "csrf"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal("other session create failed")
	}
	out, stderr, err = run("reset-admin-password")
	if err != nil || stderr != "" {
		t.Fatal("reset must succeed without diagnostic output")
	}
	pw := strings.TrimSuffix(out, "\n")
	if !password.ValidLength(pw) || strings.ContainsAny(pw, "\r\n ") || out != pw+"\n" || strings.Contains(out, "pbkdf2") || strings.Contains(out, hash) || pw == oldPassword {
		t.Fatal("reset must print exactly one random password, never a hash")
	}
	for _, token := range []string{"old-session-one", "old-session-two"} {
		if _, _, _, err := store.GetSession(ctx, auth.Hash(secret, token), time.Now()); err == nil {
			t.Fatal("reset left an old session active")
		}
	}
	if user, _, _, err := store.GetSession(ctx, auth.Hash(secret, "other-person-session"), time.Now()); err != nil || user.Role != "viewer" {
		t.Fatal("reset changed another person's session or role")
	}
	h := &auth.Handler{Repo: store, Local: store, PublicURL: "https://app.example", Secret: secret, Secure: true}
	mux := http.NewServeMux()
	h.Register(mux)
	login := func(p string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://app.example/auth/local/login", strings.NewReader(`{"username":"admin","password":"`+p+`"}`))
		r.Header.Set("Origin", "https://app.example")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := login(pw)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mustChangePassword":true`) {
		t.Fatal("new password must sign in with forced change")
	}
	if w := login(oldPassword); w.Code != 401 {
		t.Fatal("old password still signs in")
	}
}
