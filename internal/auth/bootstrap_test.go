package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/auth/password"
)

type bootFixture struct {
	store  *fakeLocalStore
	logs   *bytes.Buffer
	opts   BootstrapOptions
	hasher *countingHasher
}

func newBootFixture(initial string) *bootFixture {
	b := &bootFixture{store: newFakeLocalStore(), logs: &bytes.Buffer{}, hasher: &countingHasher{Hasher: password.Hasher{Iterations: 1000}}}
	b.opts = BootstrapOptions{PublicURL: "https://requests.example", InitialPassword: initial, Hasher: b.hasher, Logger: slog.New(slog.NewTextHandler(b.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	return b
}

func TestFreshInstallCreatesTheAdministratorAndLogsTheOneTimePasswordOnce(t *testing.T) {
	b := newBootFixture("")
	if err := BootstrapLocalAdmin(context.Background(), b.store, b.opts); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	cred, err := b.store.LocalCredentialByUsername(context.Background(), "admin")
	if err != nil || !cred.MustChange || cred.Disabled {
		t.Fatalf("credential read failed=%v must-change=%v disabled=%v", err != nil, cred.MustChange, cred.Disabled)
	}
	if b.store.users[cred.UserID].Role != "admin" {
		t.Fatal("the built-in account must be an administrator")
	}
	lines := strings.Split(strings.TrimSpace(b.logs.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly one log line, got %d", len(lines))
	}
	line := lines[0]
	if !strings.Contains(line, "level=INFO") {
		t.Fatal("bootstrap log is not an Info line")
	}
	pw := extractLoggedPassword(t, line)
	if utf8.RuneCountInString(pw) < 20 {
		t.Fatalf("generated password too short: %d", len(pw))
	}
	if !b.hasher.Hasher.Verify(pw, cred.PasswordHash) {
		t.Fatal("the logged password is not the stored one")
	}
	for _, want := range []string{"admin", "https://requests.example/login", "choose a new password"} {
		if !strings.Contains(line, want) {
			t.Errorf("bootstrap log missing expected instruction %q", want)
		}
	}
	if strings.Contains(line, cred.PasswordHash) || strings.Contains(line, "pbkdf2") {
		t.Fatal("the log line carries the hash")
	}
	if strings.Count(b.logs.String(), pw) != 1 {
		t.Fatal("the password must appear exactly once")
	}
	if !strings.Contains(b.store.dump(), "pbkdf2") || strings.Contains(b.store.dump(), pw) {
		t.Fatal("the store must hold only the hash")
	}
}

func extractLoggedPassword(t *testing.T, line string) string {
	t.Helper()
	_, rest, ok := strings.Cut(line, "one_time_password=")
	if !ok {
		t.Fatal("bootstrap log missing one_time_password attribute")
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		t.Fatal("bootstrap log has an empty password attribute")
	}
	rest = strings.Trim(fields[0], `"`)
	return rest
}

func TestEachFreshInstallGetsADifferentPassword(t *testing.T) {
	seen := map[string]bool{}
	for range 5 {
		b := newBootFixture("")
		if err := BootstrapLocalAdmin(context.Background(), b.store, b.opts); err != nil {
			t.Fatalf("operation failed (%T)", err)
		}
		pw := extractLoggedPassword(t, strings.TrimSpace(b.logs.String()))
		if seen[pw] {
			t.Fatal("password repeated")
		}
		seen[pw] = true
	}
}

// An upgraded instance already has people, so nothing is created. The operator
// who expected a password must be told why there is none and how to get one.
func TestExistingUsersMeanNothingIsCreatedAndTheLogSaysHowToGetOne(t *testing.T) {
	for _, initial := range []string{"", "an operator chosen password"} {
		b := newBootFixture(initial)
		b.store.addUser("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "viewer")
		if err := BootstrapLocalAdmin(context.Background(), b.store, b.opts); err != nil {
			t.Fatalf("operation failed (%T)", err)
		}
		if len(b.store.creds) != 0 {
			t.Fatalf("creds=%d: nothing may be created on an instance with users", len(b.store.creds))
		}
		lines := strings.Split(strings.TrimSpace(b.logs.String()), "\n")
		if len(lines) != 1 || !strings.Contains(lines[0], "level=INFO") {
			t.Fatalf("want exactly one Info line, got %q", b.logs.String())
		}
		for _, want := range []string{"no local administrator", "reset-admin-password"} {
			if !strings.Contains(lines[0], want) {
				t.Errorf("log line missing %q", want)
			}
		}
		if strings.Contains(b.logs.String(), initial) && initial != "" {
			t.Fatal("the supplied password reached the log")
		}
	}
}

func TestSecondStartWithAnAdministratorAlreadyThereCreatesNothingMore(t *testing.T) {
	b := newBootFixture("")
	if err := BootstrapLocalAdmin(context.Background(), b.store, b.opts); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	b.logs.Reset()
	if err := BootstrapLocalAdmin(context.Background(), b.store, b.opts); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	if b.store.created != 1 || b.logs.Len() != 0 {
		t.Fatalf("created=%d logBytes=%d", b.store.created, b.logs.Len())
	}
}

func TestEnvironmentPasswordIsUsedAndNeverLogged(t *testing.T) {
	const supplied = "operator supplied passphrase"
	b := newBootFixture(supplied)
	if err := BootstrapLocalAdmin(context.Background(), b.store, b.opts); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	cred, _ := b.store.LocalCredentialByUsername(context.Background(), "admin")
	if !b.hasher.Hasher.Verify(supplied, cred.PasswordHash) || !cred.MustChange {
		t.Fatal("the supplied password must be stored, with must-change set")
	}
	out := b.logs.String()
	if strings.Contains(out, supplied) || strings.Contains(out, "pbkdf2") {
		t.Fatal("bootstrap log contains password material")
	}
	if !strings.Contains(out, "REQUESTARR_ADMIN_PASSWORD") || !strings.Contains(out, "https://requests.example/login") {
		t.Fatal("bootstrap log missing password-source or sign-in instruction")
	}
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatal("bootstrap log must contain exactly one line")
	}
}

func TestEnvironmentPasswordOutsideTheLengthLimitsIsRefusedWithoutEchoingIt(t *testing.T) {
	for _, pw := range []string{strings.Repeat("a", 11), strings.Repeat("a", 129)} {
		b := newBootFixture(pw)
		err := BootstrapLocalAdmin(context.Background(), b.store, b.opts)
		if err == nil || !strings.Contains(err.Error(), "REQUESTARR_ADMIN_PASSWORD") || strings.Contains(err.Error(), pw) {
			t.Fatalf("invalid password error: present=%v mentions-variable=%v leaks-password=%v", err != nil, err != nil && strings.Contains(err.Error(), "REQUESTARR_ADMIN_PASSWORD"), err != nil && strings.Contains(err.Error(), pw))
		}
		if len(b.store.creds) != 0 || strings.Contains(b.logs.String(), pw) {
			t.Fatal("an invalid password created the account or was logged")
		}
	}
}

func TestPasswordIsNotLoggedWhenCreationFailsOrLosesTheRace(t *testing.T) {
	b := newBootFixture("")
	err := BootstrapLocalAdmin(context.Background(), failingCreate{b.store}, b.opts)
	if err == nil {
		t.Fatal("want the store error")
	}
	if b.logs.Len() != 0 {
		t.Fatalf("logged after a failed create: logBytes=%d", b.logs.Len())
	}
	if strings.Contains(err.Error(), "pbkdf2") {
		t.Fatal("error carries a hash")
	}
}

type failingCreate struct{ *fakeLocalStore }

func (failingCreate) CreateLocalAdmin(context.Context, string, time.Time) (bool, error) {
	return false, errors.New("database unavailable")
}

func TestThePasswordNeverReachesAnyOtherOutput(t *testing.T) {
	b := newBootFixture("")
	if err := BootstrapLocalAdmin(context.Background(), b.store, b.opts); err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	pw := extractLoggedPassword(t, strings.TrimSpace(b.logs.String()))
	// Sign in with it, fail with it, change it, then look at everything observable.
	f := newFixtureOn(t, b.store)
	bad := f.login("admin", pw+"x")
	good := f.login("admin", pw)
	cs := good.Result().Cookies()
	me := f.authed("GET", "/auth/me", "", cs)
	chg := f.authed("POST", "/auth/local/password", changeBody(pw, goodNewPassword), cs)
	for name, out := range map[string]string{
		"failed sign-in":  bad.Body.String() + headersOf(bad),
		"sign-in":         good.Body.String() + headersOf(good),
		"current user":    me.Body.String() + headersOf(me),
		"password change": chg.Body.String() + headersOf(chg),
		"handler log":     f.logs.String(),
		"audit rows":      b.store.auditDump(),
		"stored state":    b.store.dump(),
	} {
		if strings.Contains(out, pw) {
			t.Errorf("%s contains the one-time password", name)
		}
		if name != "stored state" && strings.Contains(out, "pbkdf2") {
			t.Errorf("%s contains a hash", name)
		}
	}
	if chg.Code != 204 {
		t.Fatalf("password change status=%d", chg.Code)
	}
}
