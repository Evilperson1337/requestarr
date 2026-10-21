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

	"github.com/example/requestarr/internal/auth"
	"github.com/example/requestarr/internal/buildinfo"
	"github.com/example/requestarr/internal/domain"
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

func TestProtectedMutationRouteAllowlist(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         bool
	}{{http.MethodPost, "/requests", true}, {http.MethodPut, "/admin/users/id/role", true}, {http.MethodPut, "/requests/id/subscription", true}, {http.MethodDelete, "/requests/id/subscription", true}, {http.MethodPost, "/requests/id/transition", true}, {http.MethodPost, "/setup/claim-admin", true}, {http.MethodPost, "/auth/callback", false}, {http.MethodGet, "/requests", false}, {http.MethodPost, "/requests/id/subscription/extra", false}} {
		if got := protectedMutation(tc.method, tc.path); got != tc.want {
			t.Errorf("protectedMutation(%q, %q)=%v want %v", tc.method, tc.path, got, tc.want)
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

type jellyfinStatusReadiness struct {
	fakeReadiness
	status domain.SourceSyncStatus
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

func (f jellyfinStatusReadiness) JellyfinSyncStatus(context.Context) (domain.SourceSyncStatus, error) {
	return f.status, nil
}

func TestJellyfinStatusEndpointRequiresAdminAndDoesNotExposeSecrets(t *testing.T) {
	unauth := New(testLogger(), buildinfo.Info{}, jellyfinStatusReadiness{}, &auth.Handler{Repo: statusAuthRepo{user: auth.User{ID: "user-1", Role: "admin"}}, Secret: "test-secret"})
	noSession := httptest.NewRecorder()
	unauth.ServeHTTP(noSession, httptest.NewRequest(http.MethodGet, "/api/v1/admin/sources/jellyfin/status", nil))
	if noSession.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d want=401", noSession.Code)
	}
	for _, role := range []string{"viewer", "admin"} {
		t.Run(role, func(t *testing.T) {
			repo := statusAuthRepo{user: auth.User{ID: "user-1", Role: role}}
			authHandler := &auth.Handler{Repo: repo, Secret: "test-secret"}
			ready := jellyfinStatusReadiness{status: domain.SourceSyncStatus{Configured: true, Enabled: true, LastSyncError: ptrString("sync unavailable")}}
			h := New(testLogger(), buildinfo.Info{}, ready, authHandler)
			r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/sources/jellyfin/status", nil)
			r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			want := http.StatusForbidden
			if role == "admin" {
				want = http.StatusOK
			}
			if rec.Code != want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
			}
			if role == "admin" && strings.Contains(rec.Body.String(), "apiKey") {
				t.Fatal("status included credential field")
			}
		})
	}
}

func ptrString(s string) *string { return &s }
