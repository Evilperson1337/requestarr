package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/requestarr/internal/auth/oidc"
)

type authRepoFake struct {
	preauth  string
	consumed bool
}

func (f *authRepoFake) SaveTransaction(_ context.Context, _, _, _, preauth string, _ time.Time) error {
	f.preauth = preauth
	return nil
}
func (f *authRepoFake) ConsumeTransaction(_ context.Context, _, preauth string, _ time.Time) (string, string, error) {
	if preauth != f.preauth || f.consumed {
		return "", "", errors.New("invalid")
	}
	f.consumed = true
	return "n", "v", nil
}
func (*authRepoFake) Login(context.Context, string, string, string, string, string, time.Time) (User, error) {
	return User{}, nil
}
func (*authRepoFake) CreateSession(context.Context, string, []byte, []byte, time.Time) error {
	return nil
}
func (*authRepoFake) GetSession(context.Context, []byte, time.Time) (User, []byte, time.Time, error) {
	return User{}, nil, time.Time{}, errors.New("missing")
}
func (*authRepoFake) RevokeSession(context.Context, []byte, time.Time) error { return nil }

type authProviderFake struct{}

func (authProviderFake) AuthorizationURL(s, _, _ string) string {
	return "https://id.example/?state=" + s
}
func (authProviderFake) Exchange(context.Context, string, string, string) (*oidc.Identity, error) {
	return nil, errors.New("unused")
}

func TestSameOrigin(t *testing.T) {
	for _, tt := range []struct {
		origin string
		want   bool
	}{{"https://app.example", true}, {"https://app.example.evil", false}, {"https://app.example/path", false}, {"null", false}, {"", false}} {
		r := httptest.NewRequest("POST", "https://app.example/api", nil)
		r.Header.Set("Origin", tt.origin)
		if got := sameOrigin(r, "https://app.example"); got != tt.want {
			t.Errorf("sameOrigin(%q)=%v, want %v", tt.origin, got, tt.want)
		}
	}
}

func TestLoginSetsBrowserBoundPreauthCookie(t *testing.T) {
	repo := &authRepoFake{}
	h := &Handler{Repo: repo, Provider: authProviderFake{}, Secret: "secret", PublicURL: "https://app.example"}
	rec := httptest.NewRecorder()
	h.login(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d", rec.Code)
	}
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == preauthCookie && c.HttpOnly && c.MaxAge == 300 {
			found = true
		}
	}
	if !found || repo.preauth == "" {
		t.Fatal("login did not set/store browser pre-auth secret")
	}
}

func TestClaimRequiresValidBodyAndVerifiedIdentity(t *testing.T) {
	repo := &authRepoFake{}
	h := &Handler{Repo: repo, Secret: "secret"}
	rec := httptest.NewRecorder()
	h.claimAdmin(rec, httptest.NewRequest(http.MethodPost, "/setup/claim-admin", strings.NewReader(`{"issuer":"x","subject":"y","token":"z"}`)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
}

func TestCSRFMiddlewareRejectsMissingOrInvalidSession(t *testing.T) {
	h := &Handler{Repo: &authRepoFake{}, Secret: "secret", PublicURL: "https://app.example"}
	nextCalled := false
	middleware := h.CSRFMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true }))
	for _, cookie := range []string{"", sessionCookie + "=invalid"} {
		r := httptest.NewRequest(http.MethodPost, "https://app.example/setup/claim-admin", nil)
		r.Header.Set("Origin", "https://app.example")
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		middleware.ServeHTTP(rec, r)
		if rec.Code != http.StatusUnauthorized || nextCalled {
			t.Fatalf("cookie=%q status=%d nextCalled=%v", cookie, rec.Code, nextCalled)
		}
	}
}
