package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth/password"
)

const (
	testPublicURL = "https://app.example"
	adminUserID   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testPassword  = "correct horse battery staple"
	testSecret    = "test-secret"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// countingHasher counts the hashing work done, so a test can see that an
// unknown username costs the same as a known one.
type countingHasher struct {
	password.Hasher
	mu       sync.Mutex
	verifies int
	hashes   int
}

func (c *countingHasher) Verify(pw, enc string) bool {
	c.mu.Lock()
	c.verifies++
	c.mu.Unlock()
	return c.Hasher.Verify(pw, enc)
}
func (c *countingHasher) Hash(pw string) (string, error) {
	c.mu.Lock()
	c.hashes++
	c.mu.Unlock()
	return c.Hasher.Hash(pw)
}
func (c *countingHasher) reset() { c.mu.Lock(); c.verifies, c.hashes = 0, 0; c.mu.Unlock() }

type fakeSession struct {
	user    User
	csrf    []byte
	expires time.Time
	revoked bool
}

// fakeLocalStore is an in-memory Repository and LocalStore.
type fakeLocalStore struct {
	mu        sync.Mutex
	users     map[string]User // by id; the stored role is authoritative
	creds     map[string]*LocalCredential
	sessions  map[string]*fakeSession // by token hash
	events    []AuditEvent
	created   int
	failAudit bool

	prunedBefore time.Time
}

func newFakeLocalStore() *fakeLocalStore {
	return &fakeLocalStore{users: map[string]User{}, creds: map[string]*LocalCredential{}, sessions: map[string]*fakeSession{}}
}

func (f *fakeLocalStore) addLocalAdmin(t *testing.T, hasher password.Hasher, pw string, mustChange bool) {
	t.Helper()
	enc, err := hasher.Hash(pw)
	if err != nil {
		t.Fatalf("operation failed (%T)", err)
	}
	f.users[adminUserID] = User{ID: adminUserID, Role: "admin", DisplayName: "Administrator"}
	f.creds[adminUserID] = &LocalCredential{UserID: adminUserID, Username: "admin", PasswordHash: enc, MustChange: mustChange}
}

func (f *fakeLocalStore) addUser(id, role string) { f.users[id] = User{ID: id, Role: role} }

func (f *fakeLocalStore) eventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, e := range f.events {
		out = append(out, e.Type)
	}
	return out
}

func (f *fakeLocalStore) liveSessions(userID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.sessions {
		if s.user.ID == userID && !s.revoked {
			n++
		}
	}
	return n
}

func (f *fakeLocalStore) LocalCredentialByUsername(_ context.Context, name string) (LocalCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.creds {
		if c.Username == name {
			return *c, nil
		}
	}
	return LocalCredential{}, ErrNoCredential
}
func (f *fakeLocalStore) LocalCredentialByUserID(_ context.Context, id string) (LocalCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.creds[id]; ok {
		return *c, nil
	}
	return LocalCredential{}, ErrNoCredential
}
func (f *fakeLocalStore) ChangeLocalPassword(_ context.Context, userID, hash string, keep []byte, remoteAddr string, now time.Time, verify func(string) bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.creds[userID]
	if !ok {
		return ErrNoCredential
	}
	if verify != nil && !verify(c.PasswordHash) {
		return ErrCurrentPasswordIncorrect
	}
	if f.failAudit {
		return errors.New("audit down")
	}
	f.events = append(f.events, AuditEvent{Type: AuditPasswordChanged, UserID: userID, Username: c.Username, RemoteAddr: remoteAddr, At: now})
	c.PasswordHash, c.MustChange = hash, false
	for token, s := range f.sessions {
		if s.user.ID == userID && token != string(keep) {
			s.revoked = true
		}
	}
	return nil
}
func (f *fakeLocalStore) RecordAuthEvent(_ context.Context, e AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAudit {
		return errors.New("audit down")
	}
	f.events = append(f.events, e)
	return nil
}
func (f *fakeLocalStore) RecordAuthEventID(_ context.Context, e AuditEvent) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAudit {
		return 0, errors.New("audit down")
	}
	e.Count = max(e.Count, 1)
	f.events = append(f.events, e)
	return int64(len(f.events)), nil
}
func (f *fakeLocalStore) AddAuthEventCount(_ context.Context, id int64, n int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events[id-1].Count += n
	return nil
}
func (f *fakeLocalStore) PruneAuthEvents(_ context.Context, before time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prunedBefore = before
	return 0, nil
}
func (f *fakeLocalStore) CreateLocalAdmin(_ context.Context, hash string, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.users) > 0 {
		return false, nil
	}
	f.users[adminUserID] = User{ID: adminUserID, Role: "admin"}
	f.creds[adminUserID] = &LocalCredential{UserID: adminUserID, Username: "admin", PasswordHash: hash, MustChange: true}
	f.created++
	return true, nil
}

// Repository.
func (*fakeLocalStore) SaveTransaction(context.Context, string, string, string, string, time.Time) error {
	return nil
}
func (*fakeLocalStore) ConsumeTransaction(context.Context, string, string, time.Time) (string, string, error) {
	return "", "", errors.New("unused")
}
func (*fakeLocalStore) Login(context.Context, string, string, string, string, string, time.Time) (User, error) {
	return User{}, errors.New("unused")
}
func (f *fakeLocalStore) CreateSession(_ context.Context, userID string, token, csrf []byte, exp time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[userID]
	if !ok {
		return errors.New("no such user")
	}
	f.sessions[string(token)] = &fakeSession{user: u, csrf: csrf, expires: exp}
	return nil
}
func (f *fakeLocalStore) GetSession(_ context.Context, token []byte, now time.Time) (User, []byte, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[string(token)]
	if !ok || s.revoked || !s.expires.After(now) {
		return User{}, nil, time.Time{}, errors.New("no session")
	}
	u := s.user
	if c, ok := f.creds[u.ID]; ok {
		u.MustChange = c.MustChange
	}
	return u, s.csrf, s.expires, nil
}
func (f *fakeLocalStore) RevokeSession(_ context.Context, token []byte, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[string(token)]; ok {
		s.revoked = true
	}
	return nil
}

type fixture struct {
	t      *testing.T
	h      *Handler
	store  *fakeLocalStore
	clock  *fakeClock
	hasher *countingHasher
	mux    *http.ServeMux
	logs   *bytes.Buffer

	sleepMu sync.Mutex
	slept   []time.Duration
}

// sleep stands in for holding a failed response: it records the delay (the
// fake clock is left alone so a flood stays inside one audit window).
func (f *fixture) sleep(_ context.Context, d time.Duration) {
	f.sleepMu.Lock()
	f.slept = append(f.slept, d)
	f.sleepMu.Unlock()
}

func (f *fixture) sleeps() []time.Duration {
	f.sleepMu.Lock()
	defer f.sleepMu.Unlock()
	return append([]time.Duration(nil), f.slept...)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store := newFakeLocalStore()
	f := newFixtureOn(t, store)
	store.addLocalAdmin(t, f.hasher.Hasher, testPassword, true)
	return f
}

// newFixtureOn builds the handler around an existing store.
func newFixtureOn(t *testing.T, store *fakeLocalStore) *fixture {
	t.Helper()
	f := &fixture{t: t, store: store, clock: &fakeClock{t: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}, hasher: &countingHasher{Hasher: password.Hasher{Iterations: 1000}}, logs: &bytes.Buffer{}}
	f.h = &Handler{Repo: f.store, Local: f.store, Hasher: f.hasher, Clock: f.clock.Now, Secret: testSecret, PublicURL: testPublicURL, Secure: true, Logger: slog.New(slog.NewTextHandler(f.logs, nil)), Sleep: f.sleep}
	f.mux = http.NewServeMux()
	f.h.Register(f.mux)
	f.hasher.reset()
	return f
}

// post sends JSON as a browser on the public origin would.
func (f *fixture) post(path, body string, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.RemoteAddr = "203.0.113.7:51000"
	r.Header.Set("Origin", testPublicURL)
	r.Header.Set("Content-Type", "application/json")
	for _, m := range mods {
		m(r)
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

func fromAddr(addr string) func(*http.Request) {
	return func(r *http.Request) { r.RemoteAddr = addr + ":40000" }
}

func loginBody(user, pw string) string {
	return fmt.Sprintf(`{"username":%q,"password":%q}`, user, pw)
}

func (f *fixture) login(user, pw string, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	return f.post("/auth/local/login", loginBody(user, pw), mods...)
}

// signIn signs in successfully and returns the session cookies.
func (f *fixture) signIn() []*http.Cookie {
	f.t.Helper()
	w := f.login("admin", testPassword)
	if w.Code != http.StatusOK {
		f.t.Fatalf("sign-in status=%d bodyBytes=%d", w.Code, w.Body.Len())
	}
	return w.Result().Cookies()
}

// authed sends an authenticated request with the cookies and the CSRF header.
func (f *fixture) authed(method, path, body string, cookies []*http.Cookie, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "203.0.113.7:51000"
	r.Header.Set("Origin", testPublicURL)
	r.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		r.AddCookie(c)
		if c.Name == "requestarr_csrf" {
			r.Header.Set("X-CSRF-Token", c.Value)
		}
	}
	for _, m := range mods {
		m(r)
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

func headersOf(w *httptest.ResponseRecorder) string {
	var b strings.Builder
	for k, v := range w.Result().Header {
		b.WriteString(k + ": " + strings.Join(v, ",") + "\n")
	}
	return b.String()
}

// dump renders everything the fake holds, as the database would.
func (f *fakeLocalStore) dump() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b strings.Builder
	for _, c := range f.creds {
		fmt.Fprintf(&b, "%+v ", *c)
	}
	return fmt.Sprintf("%+v %s %+v", f.users, b.String(), f.events)
}

func (f *fakeLocalStore) auditDump() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fmt.Sprintf("%+v", f.events)
}

func (f *fakeLocalStore) CreateLocalSession(_ context.Context, cred LocalCredential, token, csrf []byte, exp time.Time, remoteAddr string, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.creds[cred.UserID]
	if !ok || c.Disabled || c.PasswordHash != cred.PasswordHash {
		return ErrNoCredential
	}
	if f.failAudit {
		return errors.New("audit down")
	}
	f.sessions[string(token)] = &fakeSession{user: f.users[cred.UserID], csrf: csrf, expires: exp}
	f.events = append(f.events, AuditEvent{Type: AuditSignInSuccess, UserID: cred.UserID, Username: c.Username, RemoteAddr: remoteAddr, At: now})
	return nil
}
