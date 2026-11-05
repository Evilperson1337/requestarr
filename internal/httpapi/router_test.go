package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
)

func TestRequireJSONEOF(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{{`{"url":"x"}`, true}, {`{"url":"x"} {}`, false}, {`{"url":"x"} trailing`, false}} {
		decoder := json.NewDecoder(strings.NewReader(tc.body))
		var payload map[string]string
		if err := decoder.Decode(&payload); err != nil {
			t.Fatalf("decode test payload: %v", err)
		}
		err := requireJSONEOF(decoder)
		if (err == nil) != tc.ok {
			t.Errorf("requireJSONEOF(%q) error = %v", tc.body, err)
		}
	}
}

type fakeReadiness struct {
	err error
}

func (f fakeReadiness) Ready(context.Context) error { return f.err }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealthzAlwaysOK(t *testing.T) {
	handler := New(testLogger(), buildinfo.Info{}, fakeReadiness{err: errors.New("db down")})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestEmbeddedSPAAndRouteBoundaries(t *testing.T) {
	h := New(testLogger(), buildinfo.Info{}, fakeReadiness{})
	for _, tc := range []struct {
		path, contentType string
		status            int
	}{{"/", "text/html", 200}, {"/requests", "text/html", 200}, {"/missing.js", "", 404}, {"/auth/not-real", "", 404}} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.status {
			t.Errorf("%s status=%d want %d", tc.path, rec.Code, tc.status)
		}
		if tc.contentType != "" && !strings.Contains(rec.Header().Get("Content-Type"), tc.contentType) {
			t.Errorf("%s content-type=%q", tc.path, rec.Header().Get("Content-Type"))
		}
	}
}

func TestReadyzReflectsChecker(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "ready", err: nil, want: http.StatusOK},
		{name: "not ready", err: errors.New("db down"), want: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := New(testLogger(), buildinfo.Info{}, fakeReadiness{err: tt.err})

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if rec.Code != tt.want {
				t.Fatalf("readyz status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestProductAPIRoutesFailClosedWithoutAuthentication(t *testing.T) {
	handler := New(testLogger(), buildinfo.Info{}, fakeReadiness{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/requests", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// The route table is the single source of access and CSRF policy: every route
// that changes state is CSRF-protected (the auth routes that check it
// themselves excepted) and no administrator route is public.
func TestRouteTablePolicy(t *testing.T) {
	_, rt := newRouter(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Secret: "test-secret"})
	policies := rt.Policies()
	if len(policies) < 25 {
		t.Fatalf("only %d routes registered", len(policies))
	}
	want := map[string]routePolicy{
		"POST /requests":                         {Access: accessSession, CSRF: true},
		"GET /requests":                          {Access: accessSession},
		"PUT /requests/{requestID}/subscription": {Access: accessSession, CSRF: true},
		"POST /requests/{requestID}/transition":  {Access: accessSession, CSRF: true},
		"POST /admin/sources/{source}/sync":      {Access: accessAdmin, CSRF: true},
		"PUT /admin/users/{userID}/role":         {Access: accessAdmin, CSRF: true},
		"POST /admin/checklist/dismiss":          {Access: accessAdmin, CSRF: true},
		"GET /admin/checklist":                   {Access: accessAdmin},
		"PUT /admin/settings/igdb":               {Access: accessAdmin, CSRF: true},
		"POST /setup/claim-admin":                {Access: accessSession, CSRF: true},
		"POST /auth/logout":                      {Access: accessSession},
		"GET /auth/login":                        {Access: accessPublic},
		"GET /version":                           {Access: accessPublic},
		"GET /setup/status":                      {Access: accessPublic},
		"GET /search":                            {Access: accessSession},
		"GET /search/types":                      {Access: accessSession},
	}
	byPattern := map[string]routePolicy{}
	for _, p := range policies {
		byPattern[p.Pattern] = p
		if strings.HasPrefix(p.Pattern, "GET /admin/") && p.Access != accessAdmin || strings.Contains(p.Pattern, " /admin/") && p.Access != accessAdmin {
			t.Errorf("%s is not administrator-only", p.Pattern)
		}
		if mutating(p.Pattern) && p.Access != accessPublic && !p.CSRF && p.Pattern != "POST /auth/logout" {
			t.Errorf("%s changes state without CSRF protection", p.Pattern)
		}
		if p.CSRF && !mutating(p.Pattern) {
			t.Errorf("%s needs CSRF but does not change state", p.Pattern)
		}
	}
	for pattern, policy := range want {
		got, ok := byPattern[pattern]
		if !ok || got.Access != policy.Access || got.CSRF != policy.CSRF {
			t.Errorf("%s = %+v (registered %v), want access %v csrf %v", pattern, got, ok, policy.Access, policy.CSRF)
		}
	}
}

func TestUnknownAPIPathReturnsNotFound(t *testing.T) {
	handler := New(testLogger(), buildinfo.Info{}, fakeReadiness{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestPublicAndUnknownAPIPathsHaveExpectedAuthBoundary(t *testing.T) {
	handler := New(testLogger(), buildinfo.Info{}, fakeReadiness{})
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/v1/version", http.StatusOK},
		{"/api/v1/setup/status", http.StatusOK},
		{"/api/v1/not-a-route", http.StatusUnauthorized},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestUnknownAPIPathRequiresAuthenticationWhenAuthConfigured(t *testing.T) {
	// With no handler configured, the API remains closed by returning 404; a
	// concrete auth handler's default-deny middleware is exercised in auth tests.
	handler := New(testLogger(), buildinfo.Info{}, fakeReadiness{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/not-a-route", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
}

func TestKnownProtectedAPIEndpointFailsClosedWithoutAuthentication(t *testing.T) {
	handler := New(testLogger(), buildinfo.Info{}, fakeReadiness{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/requests", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

type statusAuthRepo struct{ user auth.User }

func (statusAuthRepo) SaveTransaction(context.Context, string, string, string, string, time.Time) error {
	return nil
}
func (statusAuthRepo) ConsumeTransaction(context.Context, string, string, time.Time) (string, string, error) {
	return "", "", errors.New("unused")
}
func (statusAuthRepo) Login(context.Context, string, string, string, string, string, time.Time) (auth.User, error) {
	return auth.User{}, errors.New("unused")
}
func (statusAuthRepo) CreateSession(context.Context, string, []byte, []byte, time.Time) error {
	return nil
}
func (f statusAuthRepo) GetSession(context.Context, []byte, time.Time) (auth.User, []byte, time.Time, error) {
	return f.user, []byte("csrf"), time.Now().Add(time.Hour), nil
}
func (statusAuthRepo) RevokeSession(context.Context, []byte, time.Time) error { return nil }

type roleChangeReadiness struct {
	fakeReadiness
	err   error
	calls []string
}

func (f *roleChangeReadiness) ChangeUserRole(_ context.Context, actor, target, role string, _ time.Time) error {
	f.calls = append(f.calls, actor+":"+target+":"+role)
	return f.err
}

type csrfAuthRepo struct{ statusAuthRepo }

func (f csrfAuthRepo) GetSession(context.Context, []byte, time.Time) (auth.User, []byte, time.Time, error) {
	return f.user, auth.Hash("test-secret", "csrf-token"), time.Now().Add(time.Hour), nil
}

func TestRoleChangeEndpointAcceptsViewerAndReportsGuards(t *testing.T) {
	const target = "11111111-1111-1111-1111-111111111111"
	for _, tc := range []struct {
		name, body string
		err        error
		want       int
		wantCalls  int
	}{
		{"viewer", `{"role":"viewer"}`, nil, http.StatusNoContent, 1},
		{"requester", `{"role":"requester"}`, nil, http.StatusNoContent, 1},
		{"admin", `{"role":"admin"}`, nil, http.StatusNoContent, 1},
		{"unknown role", `{"role":"owner"}`, nil, http.StatusBadRequest, 0},
		{"last admin", `{"role":"viewer"}`, postgres.ErrLastAdmin, http.StatusConflict, 1},
		{"self", `{"role":"viewer"}`, postgres.ErrAdminSelfRoleChange, http.StatusConflict, 1},
		{"missing user", `{"role":"viewer"}`, postgres.ErrAdminUserNotFound, http.StatusNotFound, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ready := &roleChangeReadiness{err: tc.err}
			h := New(testLogger(), buildinfo.Info{}, ready, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: "admin-1", Role: "admin"}}}, Secret: "test-secret", PublicURL: "http://example.test"})
			r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+target+"/role", strings.NewReader(tc.body))
			r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
			r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
			r.Header.Set("X-CSRF-Token", "csrf-token")
			r.Header.Set("Origin", "http://example.test")
			r.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != tc.want || len(ready.calls) != tc.wantCalls {
				t.Fatalf("status=%d want=%d calls=%v body=%s", rec.Code, tc.want, ready.calls, rec.Body.String())
			}
		})
	}
}

// A missing dependency stops startup instead of leaving a route that answers
// 503 later.
func TestNewRouterNamesEveryMissingDependency(t *testing.T) {
	if _, err := NewRouter(Deps{}); err == nil || !strings.Contains(err.Error(), "Logger") || !strings.Contains(err.Error(), "Checklist") || !strings.Contains(err.Error(), "Secrets") {
		t.Fatalf("err = %v", err)
	}
	full := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Secret: "test-secret"})
	if _, err := NewRouter(full); err != nil {
		t.Fatalf("complete Deps rejected: %v", err)
	}
	full.Inbox = nil
	if _, err := NewRouter(full); err == nil || !strings.Contains(err.Error(), "Inbox") || strings.Contains(err.Error(), "Logger") {
		t.Fatalf("err = %v, want only Inbox missing", err)
	}
}

type peopleReadiness struct {
	fakeReadiness
	role  domain.Role
	err   error
	calls []domain.Role
}

func (f *peopleReadiness) DefaultFirstSignInRole(context.Context) (domain.Role, error) {
	if f.role == "" {
		return "viewer", nil
	}
	return f.role, nil
}

func (f *peopleReadiness) SetDefaultFirstSignInRole(_ context.Context, role domain.Role) error {
	f.calls = append(f.calls, role)
	if role != "viewer" && role != "requester" {
		return ErrInvalidDefaultRole // the real store's contract
	}
	if f.err == nil {
		f.role = role
	}
	return f.err
}

func peopleRequest(t *testing.T, ready *peopleReadiness, role, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := New(testLogger(), buildinfo.Info{}, ready, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: "u-1", Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(method, "/api/v1/admin/settings/people", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("X-CSRF-Token", "csrf-token")
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestPeopleSettingIsAdminOnlyAndNeverAdmin(t *testing.T) {
	for _, role := range []string{"viewer", "requester"} {
		ready := &peopleReadiness{}
		if rec := peopleRequest(t, ready, role, http.MethodGet, ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s GET status=%d, want 403", role, rec.Code)
		}
		if rec := peopleRequest(t, ready, role, http.MethodPut, `{"defaultRole":"requester"}`); rec.Code != http.StatusForbidden || len(ready.calls) != 0 {
			t.Errorf("%s PUT status=%d calls=%v, want 403 and no change", role, rec.Code, ready.calls)
		}
	}
	ready := &peopleReadiness{}
	rec := peopleRequest(t, ready, "admin", http.MethodGet, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"defaultRole":"viewer"}` {
		t.Fatalf("GET status=%d body=%s, want viewer default", rec.Code, rec.Body.String())
	}
	if rec := peopleRequest(t, ready, "admin", http.MethodPut, `{"defaultRole":"requester"}`); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"defaultRole":"requester"}` || len(ready.calls) != 1 {
		t.Fatalf("PUT requester status=%d body=%s calls=%v", rec.Code, rec.Body.String(), ready.calls)
	}
	for bad, code := range map[string]string{`{"defaultRole":"admin"}`: "invalid_default_role", `{"defaultRole":"owner"}`: "invalid_default_role", `{}`: "invalid_default_role", `{"defaultRole":"viewer","x":1}`: "invalid_json", `nope`: "invalid_json", `{"defaultRole":7}`: "invalid_json"} {
		ready := &peopleReadiness{}
		rec := peopleRequest(t, ready, "admin", http.MethodPut, bad)
		assertErrorEnvelope(t, rec, http.StatusBadRequest, code)
		if ready.role != "" {
			t.Errorf("%s changed the stored default to %q", bad, ready.role)
		}
	}
}
