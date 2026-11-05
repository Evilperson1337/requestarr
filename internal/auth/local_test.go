package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("not an error envelope: %q", w.Body.String())
	}
	return body.Error.Code
}

func cookie(cs []*http.Cookie, name string) *http.Cookie {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// ---- Sign-in: the negative cases come first.

func TestSignInFailuresAreUniformAndDoTheSameHashingWork(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*fixture)
		user  string
		pass  string
	}{
		{"wrong password", func(*fixture) {}, "admin", "not the password at all"},
		{"unknown username", func(*fixture) {}, "nobody", testPassword},
		{"unknown username, empty password", func(*fixture) {}, "nobody", ""},
		{"disabled account, right password", func(f *fixture) { f.store.creds[adminUserID].Disabled = true }, "admin", testPassword},
		{"empty username", func(*fixture) {}, "", testPassword},
	}
	var first *httptest.ResponseRecorder
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.setup(f)
			w := f.login(tc.user, tc.pass)
			if w.Code != http.StatusUnauthorized || errorCode(t, w) != "invalid_credentials" {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if f.hasher.verifies != 1 {
				t.Fatalf("hash verifications=%d, want exactly 1 whatever was wrong", f.hasher.verifies)
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("a failed sign-in set cookies")
			}
			if len(f.store.sessions) != 0 {
				t.Fatal("a failed sign-in created a session")
			}
			if first == nil {
				first = w
			}
			var a, b struct {
				Error struct{ Code, Message string }
			}
			_ = json.Unmarshal(first.Body.Bytes(), &a)
			_ = json.Unmarshal(w.Body.Bytes(), &b)
			if a != b {
				t.Fatalf("responses differ: %+v vs %+v", a, b)
			}
			if m := strings.ToLower(b.Error.Message); strings.Contains(m, "unknown") || strings.Contains(m, "disabled") || strings.Contains(m, "user not") {
				t.Fatalf("message gives the reason away: %q", b.Error.Message)
			}
		})
	}
}

func TestSignInRejectsRequestsThatCouldBeForgedAcrossSites(t *testing.T) {
	for name, mod := range map[string]func(*http.Request){
		"no origin":            func(r *http.Request) { r.Header.Del("Origin") },
		"foreign origin":       func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		"null origin":          func(r *http.Request) { r.Header.Set("Origin", "null") },
		"origin with a suffix": func(r *http.Request) { r.Header.Set("Origin", testPublicURL+".evil.example") },
		"form content type":    func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") },
		"plain text type":      func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		"no content type":      func(r *http.Request) { r.Header.Del("Content-Type") },
		"json lookalike":       func(r *http.Request) { r.Header.Set("Content-Type", "application/jsonp") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			w := f.login("admin", testPassword, mod)
			if w.Code != http.StatusForbidden && w.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status=%d, want a refusal", w.Code)
			}
			if len(f.store.sessions) != 0 || len(w.Result().Cookies()) != 0 {
				t.Fatal("a refused request still signed in")
			}
			if f.hasher.verifies != 0 {
				t.Fatal("a refused request cost hashing work or tested the password")
			}
		})
	}
}

func TestSignInOnlyAcceptsPostWithAWellFormedBody(t *testing.T) {
	f := newFixture(t)
	r := httptest.NewRequest(http.MethodGet, "/auth/local/login", nil)
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", w.Code)
	}
	for name, body := range map[string]string{
		"not json":      "username=admin&password=x",
		"unknown field": `{"username":"admin","password":"x","remember":true}`,
		"trailing json": `{"username":"admin","password":"x"}{}`,
		"too large":     `{"username":"admin","password":"` + strings.Repeat("a", 8<<10) + `"}`,
		"wrong types":   `{"username":1,"password":2}`,
		"empty":         ``,
	} {
		w := f.post("/auth/local/login", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s", name, w.Code, w.Body.String())
		}
	}
	if len(f.store.sessions) != 0 {
		t.Fatal("a malformed body signed in")
	}
}

func TestSignInWithoutALocalStoreIsUnavailableNotOpen(t *testing.T) {
	h := &Handler{Repo: newFakeLocalStore(), Secret: testSecret, PublicURL: testPublicURL}
	mux := http.NewServeMux()
	h.Register(mux)
	r := httptest.NewRequest(http.MethodPost, "/auth/local/login", strings.NewReader(loginBody("admin", testPassword)))
	r.Header.Set("Origin", testPublicURL)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestSignInSucceedsWithTheOpaqueSessionMechanism(t *testing.T) {
	f := newFixture(t)
	w := f.login("admin", testPassword)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	cs := w.Result().Cookies()
	session, csrf := cookie(cs, sessionCookie), cookie(cs, "requestarr_csrf")
	if session == nil || csrf == nil {
		t.Fatal("session and CSRF cookies are required")
	}
	if !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || session.Path != "/" {
		t.Fatalf("session cookie flags: %+v", session)
	}
	if !csrf.Secure || csrf.SameSite != http.SameSiteLaxMode {
		t.Fatalf("csrf cookie flags: %+v", csrf)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("sign-in responses must not be cached")
	}
	if _, ok := f.store.sessions[testSecretHashRaw(session.Value)]; !ok {
		t.Fatal("the session is not stored by the hash of its token")
	}
	for token := range f.store.sessions {
		if token == session.Value {
			t.Fatal("the plaintext session token is stored")
		}
	}
	var body struct {
		User               User `json:"user"`
		MustChangePassword bool `json:"mustChangePassword"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.User.Role != "admin" || !body.MustChangePassword {
		t.Fatalf("body=%s err=%v", w.Body.String(), err)
	}
	if strings.Contains(w.Body.String(), testPassword) || strings.Contains(w.Body.String(), "pbkdf2") {
		t.Fatal("the response carries password material")
	}
}

func testSecretHashRaw(token string) string { return string(Hash(testSecret, token)) }

func TestAuthMeReportsTheMustChangeFlag(t *testing.T) {
	f := newFixture(t)
	cs := f.signIn()
	w := f.authed(http.MethodGet, "/auth/me", "", cs)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct{ MustChangePassword *bool }
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.MustChangePassword == nil || !*body.MustChangePassword {
		t.Fatalf("mustChangePassword missing or false: %s", w.Body.String())
	}
	f.store.creds[adminUserID].MustChange = false
	w = f.authed(http.MethodGet, "/auth/me", "", cs)
	body.MustChangePassword = nil
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.MustChangePassword == nil || *body.MustChangePassword {
		t.Fatalf("flag should be present and false: %s", w.Body.String())
	}
}

// ---- Audit.

func TestEverySignInSuccessAndFailureIsAuditedWithoutPasswordMaterial(t *testing.T) {
	f := newFixture(t)
	f.login("admin", "wrong password entirely")
	f.login("someone-else", "another wrong one")
	f.login("pasted-a-password-into-the-name-box", "x")
	f.store.creds[adminUserID].Disabled = true
	f.login("admin", testPassword)
	f.store.creds[adminUserID].Disabled = false
	f.clock.Advance(time.Hour)
	f.login("admin", testPassword)
	want := []string{AuditSignInFailure, AuditSignInFailure, AuditSignInFailure, AuditSignInFailure, AuditSignInSuccess}
	got := f.store.eventTypes()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events=%v want %v", got, want)
	}
	for _, e := range f.store.events {
		dump := e.Type + "|" + e.UserID + "|" + e.Username + "|" + e.RemoteAddr
		for _, secret := range []string{testPassword, "wrong password entirely", "another wrong one", "pbkdf2"} {
			if strings.Contains(dump, secret) {
				t.Fatalf("audit row carries %q: %+v", secret, e)
			}
		}
		if e.RemoteAddr != "203.0.113.7" {
			t.Fatalf("audit address=%q, want the host without the port", e.RemoteAddr)
		}
		if e.Username != "" && e.Username != "admin" {
			t.Fatalf("an unrecognised username (maybe a mistyped password) was stored: %q", e.Username)
		}
		if e.At.IsZero() {
			t.Fatal("audit row has no time")
		}
	}
	if last := f.store.events[4]; last.UserID != adminUserID || last.Username != "admin" {
		t.Fatalf("success row: %+v", last)
	}
}

func TestSuccessfulSignInFailsClosedWhenItCannotBeAudited(t *testing.T) {
	f := newFixture(t)
	f.store.failAudit = true
	w := f.login("admin", testPassword)
	if w.Code == http.StatusOK || len(f.store.sessions) != 0 {
		t.Fatalf("signed in without an audit record: status=%d", w.Code)
	}
}

// ---- Delay.

func retryAfter(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	n, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || n < 1 {
		t.Fatalf("missing Retry-After on %d: %q", w.Code, w.Header().Get("Retry-After"))
	}
	return n
}

func TestFailedAttemptsAddAnExponentiallyGrowingCappedDelayPerAccount(t *testing.T) {
	f := newFixture(t)
	var delays []int
	for i := 0; i < 30; i++ {
		attempt := func() *httptest.ResponseRecorder {
			return f.login("admin", "wrong password "+strconv.Itoa(i), fromAddr("198.51.100."+strconv.Itoa(i+1)))
		}
		w := attempt()
		if w.Code == http.StatusTooManyRequests {
			if errorCode(t, w) != "too_many_attempts" {
				t.Fatalf("code=%q", errorCode(t, w))
			}
			d := retryAfter(t, w)
			delays = append(delays, d)
			f.clock.Advance(time.Duration(d) * time.Second)
			w = attempt()
		}
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status=%d after serving the delay", i, w.Code)
		}
	}
	if len(delays) < 6 {
		t.Fatalf("delays=%v: too few failures were delayed", delays)
	}
	for i := 1; i < len(delays); i++ {
		if delays[i] < delays[i-1] {
			t.Fatalf("delay shrank: %v", delays)
		}
	}
	if delays[len(delays)-1] != delays[len(delays)-2] || delays[len(delays)-1] > 300 {
		t.Fatalf("delay is not capped at 300s: %v", delays)
	}
	grew := false
	for i := 1; i < len(delays); i++ {
		if delays[i] >= 2*delays[i-1] && delays[i] <= 300 {
			grew = true
		}
	}
	if !grew {
		t.Fatalf("delay does not grow exponentially: %v", delays)
	}
}

func TestAnAccountIsNeverLockedOut(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 60; i++ {
		f.login("admin", "wrong "+strconv.Itoa(i), fromAddr("198.51.100."+strconv.Itoa(i%250+1)))
		f.clock.Advance(10 * time.Minute) // well beyond the cap
	}
	if w := f.login("admin", testPassword); w.Code != http.StatusOK {
		t.Fatalf("after waiting out the delay the right password must work, got %d %s", w.Code, w.Body.String())
	}
}

func TestADelayedAccountRefusesEvenTheRightPasswordAndSkipsTheHashing(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 10; i++ {
		f.login("admin", "wrong", fromAddr("198.51.100."+strconv.Itoa(i+1)))
	}
	f.hasher.reset()
	w := f.login("admin", testPassword, fromAddr("198.51.100.99"))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d", w.Code)
	}
	if f.hasher.verifies != 0 {
		t.Fatal("a throttled attempt must not reach the password check")
	}
	if cookie(w.Result().Cookies(), sessionCookie) != nil {
		t.Fatal("a throttled attempt signed in")
	}
}

func TestTheDelayFollowsTheAddressAcrossUsernamesButSparesOtherAddresses(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 12; i++ {
		f.login("guess"+strconv.Itoa(i), "x", fromAddr("198.51.100.5"))
	}
	if w := f.login("admin", testPassword, fromAddr("198.51.100.5")); w.Code != http.StatusTooManyRequests {
		t.Fatalf("a noisy address must be delayed for every username, got %d", w.Code)
	}
	if w := f.login("admin", testPassword, fromAddr("198.51.100.6")); w.Code != http.StatusOK {
		t.Fatalf("a different address must be unaffected, got %d", w.Code)
	}
}

func TestDelayForUnknownUsernamesLooksLikeDelayForRealOnes(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 10; i++ {
		f.login("ghost", "x", fromAddr("198.51.100."+strconv.Itoa(i+1)))
	}
	w := f.login("ghost", "x", fromAddr("198.51.100.99"))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("unknown usernames must be throttled exactly like known ones, got %d", w.Code)
	}
}

func TestASuccessfulSignInClearsTheAccountDelay(t *testing.T) {
	f := newFixture(t)
	f.login("admin", "wrong one", fromAddr("198.51.100.1"))
	f.login("admin", "wrong two", fromAddr("198.51.100.2"))
	if w := f.login("admin", testPassword, fromAddr("198.51.100.3")); w.Code != http.StatusOK {
		t.Fatalf("within the grace allowance a typo must not block, got %d", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := f.login("admin", "wrong", fromAddr("198.51.100.4")); w.Code != http.StatusUnauthorized {
			t.Fatalf("counter was not cleared by success, got %d", w.Code)
		}
	}
}

// ---- Password change.

func changeBody(current, next string) string {
	b, _ := json.Marshal(map[string]string{"currentPassword": current, "newPassword": next})
	return string(b)
}

const goodNewPassword = "a brand new passphrase"

func TestPasswordChangeRequiresASession(t *testing.T) {
	f := newFixture(t)
	w := f.post("/auth/local/password", changeBody(testPassword, goodNewPassword))
	if w.Code != http.StatusUnauthorized || errorCode(t, w) != "authentication_required" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if f.hasher.hashes != 0 {
		t.Fatal("unauthenticated request caused hashing")
	}
}

func TestPasswordChangeRequiresCSRFAndOrigin(t *testing.T) {
	f := newFixture(t)
	cs := f.signIn()
	for name, mod := range map[string]func(*http.Request){
		"no csrf header":    func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
		"wrong csrf header": func(r *http.Request) { r.Header.Set("X-CSRF-Token", "nope") },
		"no origin":         func(r *http.Request) { r.Header.Del("Origin") },
		"foreign origin":    func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
	} {
		w := f.authed(http.MethodPost, "/auth/local/password", changeBody(testPassword, goodNewPassword), cs, mod)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status=%d", name, w.Code)
		}
	}
	if !f.store.creds[adminUserID].MustChange {
		t.Fatal("a forged request changed the password")
	}
}

func TestPasswordChangeIsRefusedForAccountsWithoutALocalCredential(t *testing.T) {
	for _, role := range []string{"viewer", "requester", "admin"} {
		t.Run(role+" through OIDC", func(t *testing.T) {
			f := newFixture(t)
			id := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
			f.store.addUser(id, role)
			if err := f.h.Repo.CreateSession(context.Background(), id, Hash(testSecret, "tok"), Hash(testSecret, "csrf"), f.clock.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			cs := []*http.Cookie{{Name: sessionCookie, Value: "tok"}, {Name: "requestarr_csrf", Value: "csrf"}}
			w := f.authed(http.MethodPost, "/auth/local/password", changeBody("anything at all", goodNewPassword), cs)
			if w.Code != http.StatusForbidden || errorCode(t, w) != "forbidden" {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if f.hasher.hashes != 0 || f.hasher.verifies != 0 {
				t.Fatal("a person without a local password reached the hasher")
			}
			if !f.store.creds[adminUserID].MustChange {
				t.Fatal("the local credential changed")
			}
		})
	}
}

func TestPasswordChangeRejectsAWrongCurrentPasswordWithTheSameWork(t *testing.T) {
	f := newFixture(t)
	cs := f.signIn()
	f.hasher.reset()
	w := f.authed(http.MethodPost, "/auth/local/password", changeBody("not my current password", goodNewPassword), cs)
	if w.Code != http.StatusForbidden || errorCode(t, w) != "current_password_incorrect" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if f.hasher.verifies != 1 {
		t.Fatalf("verifies=%d", f.hasher.verifies)
	}
	if !f.store.creds[adminUserID].MustChange || f.hasher.Verify(goodNewPassword, f.store.creds[adminUserID].PasswordHash) {
		t.Fatal("the password changed despite a wrong current password")
	}
	last := f.store.eventTypes()
	if last[len(last)-1] != AuditPasswordChangeFailure {
		t.Fatalf("events=%v", last)
	}
}

func TestRepeatedWrongCurrentPasswordsAreDelayedToo(t *testing.T) {
	f := newFixture(t)
	cs := f.signIn()
	var got429 bool
	for i := 0; i < 12; i++ {
		w := f.authed(http.MethodPost, "/auth/local/password", changeBody("wrong "+strconv.Itoa(i), goodNewPassword), cs)
		if w.Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("a stolen session can guess the current password without delay")
	}
	if w := f.authed(http.MethodPost, "/auth/local/password", changeBody(testPassword, goodNewPassword), cs); w.Code != http.StatusTooManyRequests {
		t.Fatalf("even the right password is delayed while the delay runs, got %d", w.Code)
	}
}

func TestPasswordLengthLimitsHaveNoCompositionRules(t *testing.T) {
	cases := []struct {
		name string
		pw   string
		ok   bool
	}{
		{"11 characters", strings.Repeat("a", 11), false},
		{"12 characters", strings.Repeat("a", 12), true},
		{"12 spaces", strings.Repeat(" ", 12), true},
		{"128 characters", strings.Repeat("z", 128), true},
		{"129 characters", strings.Repeat("z", 129), false},
		{"12 multi-byte characters", strings.Repeat("é", 12), true},
		{"11 multi-byte characters", strings.Repeat("é", 11), false},
		{"128 multi-byte characters", strings.Repeat("日", 128), true},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			cs := f.signIn()
			w := f.authed(http.MethodPost, "/auth/local/password", changeBody(testPassword, tc.pw), cs)
			if tc.ok && w.Code != http.StatusNoContent {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if !tc.ok {
				if w.Code != http.StatusBadRequest || errorCode(t, w) != "invalid_password" {
					t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
				}
				if !f.store.creds[adminUserID].MustChange {
					t.Fatal("an invalid password was stored")
				}
			}
		})
	}
}

func TestPasswordChangeRejectsReusingTheCurrentPassword(t *testing.T) {
	f := newFixture(t)
	cs := f.signIn()
	w := f.authed(http.MethodPost, "/auth/local/password", changeBody(testPassword, testPassword), cs)
	if w.Code != http.StatusBadRequest || errorCode(t, w) != "password_unchanged" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !f.store.creds[adminUserID].MustChange {
		t.Fatal("must-change was cleared without a new password")
	}
}

func TestPasswordChangeClearsMustChangeEndsOtherSessionsAndKeepsTheCurrentOne(t *testing.T) {
	f := newFixture(t)
	current := f.signIn()
	f.clock.Advance(time.Minute)
	other := f.signIn()
	if f.store.liveSessions(adminUserID) != 2 {
		t.Fatalf("sessions=%d", f.store.liveSessions(adminUserID))
	}
	w := f.authed(http.MethodPost, "/auth/local/password", changeBody(testPassword, goodNewPassword), current)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if f.store.creds[adminUserID].MustChange {
		t.Fatal("must-change not cleared")
	}
	if f.store.liveSessions(adminUserID) != 1 {
		t.Fatalf("live sessions=%d, want only the changing one", f.store.liveSessions(adminUserID))
	}
	if w := f.authed(http.MethodGet, "/auth/me", "", other); w.Code != http.StatusUnauthorized {
		t.Fatalf("the other session survived: %d", w.Code)
	}
	if w := f.authed(http.MethodGet, "/auth/me", "", current); w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"mustChangePassword":true`) {
		t.Fatalf("current session: %d %s", w.Code, w.Body.String())
	}
	if w := f.login("admin", testPassword); w.Code != http.StatusUnauthorized {
		t.Fatalf("the old password still works: %d", w.Code)
	}
	if w := f.login("admin", goodNewPassword, fromAddr("198.51.100.77")); w.Code != http.StatusOK {
		t.Fatalf("the new password does not work: %d", w.Code)
	}
	if got := f.store.eventTypes(); got[len(got)-3] != AuditPasswordChanged {
		t.Fatalf("events=%v", got)
	}
	for _, e := range f.store.events {
		if strings.Contains(e.Type+e.Username+e.UserID, goodNewPassword) {
			t.Fatal("audit carries the password")
		}
	}
}

func TestPasswordChangeBodyIsStrict(t *testing.T) {
	f := newFixture(t)
	cs := f.signIn()
	for name, body := range map[string]string{
		"not json":      "x",
		"unknown field": `{"currentPassword":"a","newPassword":"b","extra":1}`,
	} {
		if w := f.authed(http.MethodPost, "/auth/local/password", body, cs); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d", name, w.Code)
		}
	}
}
