package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/logbuffer"
)

// The five routes a diagnostics token may read (D043). Spelled out so adding
// one is a decision someone reads, as with the superuser-only list.
func TestTokenReadableRoutesAreExactlyThese(t *testing.T) {
	_, rt := newRouter(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Secret: "test-secret"})
	want := []string{
		"GET /admin/job-runs",
		"GET /admin/library-connections",
		"GET /admin/logs",
		"GET /admin/secrets",
		"GET /admin/status",
	}
	var got []string
	for _, p := range rt.Policies() {
		if p.TokenReadable {
			got = append(got, p.Pattern)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("token-readable routes:\n got %q\nwant %q", got, want)
	}
}

// memTokenStore is a DiagnosticsTokenStore in memory.
type memTokenStore struct {
	mu      sync.Mutex
	rows    map[string]*auth.DiagnosticsTokenRecord // by hash
	hashes  map[string]string                       // id to hash
	touches int
	lookups int
}

func newMemTokenStore() *memTokenStore {
	return &memTokenStore{rows: map[string]*auth.DiagnosticsTokenRecord{}, hashes: map[string]string{}}
}

func (s *memTokenStore) CreateDiagnosticsToken(_ context.Context, t auth.DiagnosticsToken, hash []byte, _ auth.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[string(hash)] = &auth.DiagnosticsTokenRecord{DiagnosticsToken: t, CreatorCanSignIn: true}
	s.hashes[t.ID] = string(hash)
	return nil
}
func (s *memTokenStore) ListDiagnosticsTokens(context.Context) ([]auth.DiagnosticsToken, error) {
	return nil, nil
}
func (s *memTokenStore) RevokeDiagnosticsToken(_ context.Context, id string, now time.Time, _ auth.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[s.hashes[id]].RevokedAt = &now
	return nil
}
func (s *memTokenStore) DiagnosticsTokenByHash(_ context.Context, hash []byte) (auth.DiagnosticsTokenRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookups++
	if r, ok := s.rows[string(hash)]; ok {
		return *r, nil
	}
	return auth.DiagnosticsTokenRecord{}, auth.ErrDiagnosticsTokenNotFound
}
func (s *memTokenStore) TouchDiagnosticsToken(context.Context, string, time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touches++
	return nil
}

// bearerFixture is the whole router with one live token and a clock the test moves.
type bearerFixture struct {
	h     http.Handler
	rt    *routeTable
	store *memTokenStore
	svc   *auth.DiagnosticsTokens
	logs  *bytes.Buffer
	token string
	id    string
	mu    sync.Mutex
	now   time.Time
}

func newBearerFixture(t *testing.T) *bearerFixture {
	t.Helper()
	f := &bearerFixture{store: newMemTokenStore(), logs: &bytes.Buffer{}, now: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	clock := func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
	f.svc = &auth.DiagnosticsTokens{Store: f.store, Secret: "test-secret", Clock: clock}
	buf := logbuffer.New()
	logger := slog.New(logbuffer.NewHandler(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}), buf))
	logger.Info("hello")
	handler := &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: "superuser"}}}, Secret: "test-secret", PublicURL: "http://example.test", Clock: clock, Logger: logger, Diagnostics: f.svc}
	d := testDeps(logger, buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, handler)
	d.ServerLogs = buf
	h, rt, err := buildRouter(d)
	if err != nil {
		t.Fatal(err)
	}
	f.h, f.rt = h, rt
	tok, rec, err := f.svc.Create(context.Background(), auth.User{ID: matrixUserID, Role: "superuser"}, "agent", 0, "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	f.token, f.id = tok, rec.ID
	return f
}

func (f *bearerFixture) advance(d time.Duration) { f.mu.Lock(); f.now = f.now.Add(d); f.mu.Unlock() }

// call sends one request; header "" sends no Authorization header.
func (f *bearerFixture) call(method, path, header, addr string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/v1"+path, nil)
	r.RemoteAddr = addr + ":4000"
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func (f *bearerFixture) get(path, header string) *httptest.ResponseRecorder {
	return f.call("GET", path, header, "10.0.0.9")
}

var tokenReadablePaths = []string{"/admin/logs", "/admin/status", "/admin/job-runs", "/admin/library-connections", "/admin/secrets"}

func TestAValidTokenReadsTheMarkedRoutes(t *testing.T) {
	f := newBearerFixture(t)
	for _, p := range tokenReadablePaths {
		w := f.get(p, "Bearer "+f.token)
		if w.Code == 401 || w.Code == 403 {
			t.Errorf("GET %s with a valid token = %d: %s", p, w.Code, w.Body)
		}
	}
	if w := f.get("/admin/logs", "Bearer "+f.token); w.Code != 200 || !strings.Contains(w.Body.String(), "hello") {
		t.Fatalf("logs = %d %s", w.Code, w.Body)
	}
	// The scheme name is case-insensitive.
	if w := f.get("/admin/logs", "bearer "+f.token); w.Code != 200 {
		t.Fatalf("lower-case scheme = %d", w.Code)
	}
}

func TestATokenIsRefusedOnEveryRouteNotMarkedAndEveryNonGet(t *testing.T) {
	f := newBearerFixture(t)
	fill := func(p string) string {
		for strings.Contains(p, "{") {
			start, end := strings.Index(p, "{"), strings.Index(p, "}")
			p = p[:start] + matrixItemID + p[end+1:]
		}
		return p
	}
	for _, p := range f.rt.Policies() {
		method, path, _ := strings.Cut(p.Pattern, " ")
		if p.TokenReadable {
			continue
		}
		w := f.call(method, fill(path), "Bearer "+f.token, "10.0.0.9")
		assertErrorEnvelope(t, w, 401, "authentication_required")
	}
	for _, path := range tokenReadablePaths {
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
			w := f.call(method, path, "Bearer "+f.token, "10.0.0.9")
			if w.Code != 401 {
				t.Errorf("%s %s with a token = %d, want 401", method, path, w.Code)
			}
		}
	}
	// An unknown path is refused the same way, not 404.
	assertErrorEnvelope(t, f.get("/no/such/route", "Bearer "+f.token), 401, "authentication_required")
}

func TestATokenInTheQueryOrACookieIsIgnoredAndTheHeaderIsJudgedAlone(t *testing.T) {
	f := newBearerFixture(t)
	assertErrorEnvelope(t, f.get("/admin/logs?access_token="+f.token+"&token="+f.token, ""), 401, "authentication_required")
	r := httptest.NewRequest("GET", "/api/v1/admin/logs", nil)
	r.AddCookie(&http.Cookie{Name: "token", Value: f.token})
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	assertErrorEnvelope(t, w, 401, "authentication_required")

	// A signed-in session does not rescue a bad token: the header decides alone.
	r = httptest.NewRequest("GET", "/api/v1/admin/logs", nil)
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	r.Header.Set("Authorization", "Bearer rqd_nothing")
	w = httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	assertErrorEnvelope(t, w, 401, "authentication_required")

	// And a valid token alongside a session cookie works as a token request.
	r = httptest.NewRequest("GET", "/api/v1/admin/logs", nil)
	r.Header.Set("Authorization", "Bearer "+f.token)
	w = httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
}

func TestEveryRefusedTokenAnswersTheSame401AndLogsAFixedReason(t *testing.T) {
	cases := []struct {
		name, reason string
		header       func(f *bearerFixture) string
		break_       func(f *bearerFixture)
	}{
		{"unknown", "unknown", func(*bearerFixture) string { return "Bearer rqd_" + strings.Repeat("A", 43) }, nil},
		{"wrong prefix", "unknown", func(f *bearerFixture) string { return "Bearer xyz_" + strings.TrimPrefix(f.token, "rqd_") }, nil},
		{"malformed empty", "malformed", func(*bearerFixture) string { return "Bearer" }, nil},
		{"malformed scheme", "malformed", func(f *bearerFixture) string { return "Basic " + f.token }, nil},
		{"malformed spaces", "malformed", func(f *bearerFixture) string { return "Bearer  " + f.token + " extra" }, nil},
		{"revoked", "revoked", func(f *bearerFixture) string { return "Bearer " + f.token }, func(f *bearerFixture) {
			_ = f.svc.Revoke(context.Background(), auth.User{ID: matrixUserID, Role: "superuser"}, f.id, "10.0.0.1")
		}},
		{"expired", "expired", func(f *bearerFixture) string { return "Bearer " + f.token }, func(f *bearerFixture) { f.advance(31 * 24 * time.Hour) }},
		{"creator demoted or disabled", "creator_not_superuser", func(f *bearerFixture) string { return "Bearer " + f.token }, func(f *bearerFixture) {
			f.store.mu.Lock()
			f.store.rows[f.store.hashes[f.id]].CreatorCanSignIn = false
			f.store.mu.Unlock()
		}},
	}
	var first string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBearerFixture(t)
			if w := f.get("/admin/logs", "Bearer "+f.token); w.Code != 200 {
				t.Fatalf("before: %d %s", w.Code, w.Body)
			}
			if tc.break_ != nil {
				tc.break_(f)
			}
			w := f.get("/admin/logs", tc.header(f))
			assertErrorEnvelope(t, w, 401, "authentication_required")
			body := w.Body.String()
			if i := strings.Index(body, "request"); i > 0 { // request ids differ
				body = body[:i]
			}
			if first == "" {
				first = body
			} else if body != first {
				t.Fatalf("body differs:\n%s\n%s", body, first)
			}
			out := f.logs.String()
			if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "reason="+tc.reason) {
				t.Fatalf("want a warning with reason=%s, log:\n%s", tc.reason, out)
			}
			if strings.Contains(out, strings.TrimPrefix(f.token, "rqd_")) {
				t.Fatalf("the token reached the log:\n%s", out)
			}
		})
	}
}

func TestRepeatedFailuresFromOneAddressAreRefusedWithoutCheckingTheToken(t *testing.T) {
	f := newBearerFixture(t)
	for i := 0; i < 6; i++ {
		assertErrorEnvelope(t, f.call("GET", "/admin/logs", "Bearer rqd_bad", "203.0.113.5"), 401, "authentication_required")
	}
	before := f.store.lookups
	w := f.call("GET", "/admin/logs", "Bearer "+f.token, "203.0.113.5")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("throttled = %d, retry-after %q", w.Code, w.Header().Get("Retry-After"))
	}
	if f.store.lookups != before {
		t.Fatal("the token was checked for a throttled address")
	}
	if !strings.Contains(f.logs.String(), "reason=throttled") {
		t.Fatalf("not logged:\n%s", f.logs)
	}
	// Another address is unaffected, and the delay ends.
	if w := f.call("GET", "/admin/logs", "Bearer "+f.token, "198.51.100.7"); w.Code != 200 {
		t.Fatalf("other address = %d", w.Code)
	}
	f.advance(time.Minute)
	if w := f.call("GET", "/admin/logs", "Bearer "+f.token, "203.0.113.5"); w.Code != 200 {
		t.Fatalf("after the delay = %d", w.Code)
	}
}

func TestATokenIsLimitedTo60RequestsAMinute(t *testing.T) {
	f := newBearerFixture(t)
	for i := 0; i < 60; i++ {
		if w := f.get("/admin/logs", "Bearer "+f.token); w.Code != 200 {
			t.Fatalf("request %d = %d", i+1, w.Code)
		}
	}
	w := f.get("/admin/logs", "Bearer "+f.token)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("61st = %d", w.Code)
	}
	if !strings.Contains(f.logs.String(), "reason=rate_limited") {
		t.Fatalf("not logged:\n%s", f.logs)
	}
	f.advance(61 * time.Second)
	if w := f.get("/admin/logs", "Bearer "+f.token); w.Code != 200 {
		t.Fatalf("next minute = %d", w.Code)
	}
}

func TestLastUsedIsWrittenAtMostOnceAMinute(t *testing.T) {
	f := newBearerFixture(t)
	for i := 0; i < 5; i++ {
		f.get("/admin/logs", "Bearer "+f.token)
	}
	if f.store.touches != 1 {
		t.Fatalf("touches = %d after 5 requests, want 1", f.store.touches)
	}
	f.advance(61 * time.Second)
	f.get("/admin/logs", "Bearer "+f.token)
	if f.store.touches != 2 {
		t.Fatalf("touches = %d after a minute, want 2", f.store.touches)
	}
}

func TestACanaryTokenNeverAppearsInALogLineOrResponse(t *testing.T) {
	f := newBearerFixture(t)
	seen := strings.Builder{}
	record := func(w *httptest.ResponseRecorder) {
		seen.WriteString(w.Body.String())
		for k, v := range w.Header() {
			seen.WriteString(k + ": " + strings.Join(v, ",") + "\n")
		}
	}
	for _, h := range []string{"Bearer " + f.token, "Bearer " + f.token + "x", "Bearer " + f.token + " junk", "Basic " + f.token} {
		for _, p := range append([]string{"/admin/users", "/requests", "/no/such"}, tokenReadablePaths...) {
			record(f.get(p+"?access_token="+f.token, h))
		}
	}
	f.advance(31 * 24 * time.Hour)
	for i := 0; i < 70; i++ {
		record(f.get("/admin/logs", "Bearer "+f.token))
	}
	secret := strings.TrimPrefix(f.token, "rqd_")
	if strings.Contains(f.logs.String(), secret) || strings.Contains(seen.String(), secret) {
		t.Fatal("the canary token appeared in a log line or a response")
	}
}
