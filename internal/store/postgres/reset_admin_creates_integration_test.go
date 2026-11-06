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

// S4: an upgraded instance with people but no administrator and no local
// administrator had no way in. The reset command, run from the shell (the
// gate), creates the built-in account when it is missing and resets it when it
// exists. Exercised through the shipped binary.
func TestResetAdminPasswordCreatesTheLocalAdministratorWhenMissingIntegration(t *testing.T) {
	ctx, store := freshStore(t)
	binary := filepath.Join(t.TempDir(), "requestarr")
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../../cmd/requestarr").CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v (%d output bytes)", err, len(out))
	}
	run := func() (string, string, error) {
		cmd := exec.CommandContext(ctx, binary, "reset-admin-password")
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "REQUESTARR_") {
				cmd.Env = append(cmd.Env, e)
			}
		}
		cmd.Env = append(cmd.Env, "REQUESTARR_DATABASE_URL="+store.pool.Config().ConnString())
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		return out.String(), stderr.String(), err
	}

	// An upgraded instance: a person who signed in through OIDC, no admin at all.
	person, err := store.Login(ctx, "https://identity.example", "someone", "", "Someone", newUUID(), time.Now())
	if err != nil || person.Role == "admin" {
		t.Fatal("fixture: expected a non-admin person")
	}
	if n := count(t, ctx, store, `SELECT count(*) FROM local_credentials`); n != 0 {
		t.Fatal("fixture: a local administrator already exists")
	}

	out, stderr, err := run()
	if err != nil {
		t.Fatalf("reset on an instance without the account must create it: %v (stderr %d bytes)", err, len(stderr))
	}
	pw := strings.TrimSuffix(out, "\n")
	if !password.ValidLength(pw) || out != pw+"\n" || strings.ContainsAny(pw, "\r\n ") || strings.Contains(out, "pbkdf2") || strings.Count(out, "\n") != 1 {
		t.Fatal("must print exactly one random password, never a hash")
	}
	if strings.Contains(stderr, pw) || strings.Contains(stderr, "pbkdf2") || !strings.Contains(stderr, "created") {
		t.Fatal("diagnostics must say it created the account and never repeat the password")
	}
	cred, err := store.LocalCredentialByUsername(ctx, "admin")
	if err != nil || !cred.MustChange || cred.Disabled || strings.Contains(cred.PasswordHash, pw) {
		t.Fatal("the created credential must force a change and hold only a hash")
	}
	if n := count(t, ctx, store, `SELECT count(*) FROM users WHERE builtin AND role='admin' AND id=$1::uuid`, cred.UserID); n != 1 {
		t.Fatal("the account must be the built-in admin user")
	}
	if n := count(t, ctx, store, `SELECT count(*) FROM auth_audit_events WHERE event_type='admin_created' AND user_id=$1::uuid`, cred.UserID); n != 1 {
		t.Fatal("creation must be audited exactly once")
	}
	if n := count(t, ctx, store, `SELECT count(*) FROM users WHERE id=$1::uuid AND role<>'viewer' AND NOT builtin`, person.ID); n != 0 {
		t.Fatal("another person's role changed")
	}

	signIn := func(pw string) int {
		h := &auth.Handler{Repo: store, Local: store, PublicURL: "https://app.example", Secret: "entrypoint-test-secret", Secure: true}
		mux := http.NewServeMux()
		h.Register(mux)
		r := httptest.NewRequest("POST", "https://app.example/auth/local/login", strings.NewReader(`{"username":"admin","password":"`+pw+`"}`))
		r.Header.Set("Origin", "https://app.example")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	if signIn(pw) != 200 {
		t.Fatal("the printed password must sign in")
	}

	// Run again: now it exists, so it resets (and does not create a second one).
	out2, stderr2, err := run()
	if err != nil || strings.Contains(stderr2, "created") {
		t.Fatalf("second run must reset, not create: %v", err)
	}
	pw2 := strings.TrimSuffix(out2, "\n")
	if pw2 == pw || out2 != pw2+"\n" {
		t.Fatal("reset must print a new single password")
	}
	if n := count(t, ctx, store, `SELECT count(*) FROM local_credentials`); n != 1 {
		t.Fatal("a second account was created")
	}
	if n := count(t, ctx, store, `SELECT count(*) FROM auth_audit_events WHERE event_type='admin_created'`); n != 1 {
		t.Fatal("a reset must not write another creation event")
	}
	if n := count(t, ctx, store, `SELECT count(*) FROM auth_audit_events WHERE event_type='password_changed'`); n != 1 {
		t.Fatal("the reset must be audited")
	}
	if signIn(pw) != 401 || signIn(pw2) != 200 {
		t.Fatal("only the new password may sign in")
	}
}

func TestResetAdminPasswordCreatesTheAccountOnAnEmptyDatabaseIntegration(t *testing.T) {
	ctx, store := freshStore(t)
	hash, err := fastHasher.Hash("a one-time password for tests")
	if err != nil {
		t.Fatal("hash failed")
	}
	created, err := store.ResetOrCreateLocalAdmin(ctx, hash, time.Now())
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	created, err = store.ResetOrCreateLocalAdmin(ctx, hash, time.Now())
	if err != nil || created {
		t.Fatalf("second call created=%v err=%v, want a reset", created, err)
	}
}
