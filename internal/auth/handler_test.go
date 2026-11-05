package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth/oidc"
)

type authRepoFake struct {
	preauth  string
	consumed bool
	loginUID string
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
func (f *authRepoFake) Login(_ context.Context, _, _, _, _, uid string, _ time.Time) (User, error) {
	f.loginUID = uid
	return User{ID: uid}, nil
}
func (*authRepoFake) CreateSession(context.Context, string, []byte, []byte, time.Time) error {
	return nil
}
func (*authRepoFake) GetSession(context.Context, []byte, time.Time) (User, []byte, time.Time, error) {
	return User{}, nil, time.Time{}, errors.New("missing")
}
func (*authRepoFake) RevokeSession(context.Context, []byte, time.Time) error { return nil }

type identityProviderFake struct{ authProviderFake }

func (identityProviderFake) Exchange(context.Context, string, string, string) (*oidc.Identity, error) {
	return &oidc.Identity{Subject: "sub-1", Email: "a@example.test", DisplayName: "A"}, nil
}

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

func TestCSRFMiddlewareRejectsMissingOrInvalidSession(t *testing.T) {
	h := &Handler{Repo: &authRepoFake{}, Secret: "secret", PublicURL: "https://app.example"}
	nextCalled := false
	middleware := h.CSRFMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true }))
	for _, cookie := range []string{"", sessionCookie + "=invalid"} {
		r := httptest.NewRequest(http.MethodPost, "https://app.example/auth/local/password", nil)
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

// users.id is a UUID column, so first sign-in must hand the repository a UUID.
func TestCallbackProvisionsUserWithUUID(t *testing.T) {
	repo := &authRepoFake{}
	h := &Handler{Repo: repo, Provider: identityProviderFake{}, Secret: "secret", PublicURL: "https://app.example"}
	loginRec := httptest.NewRecorder()
	h.login(loginRec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	state := ""
	cookies := loginRec.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "requestarr_oidc_state" {
			state = c.Value
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=c&state="+state, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.callback(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("callback status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !uuidPattern.MatchString(repo.loginUID) {
		t.Fatalf("user ID %q is not a UUID", repo.loginUID)
	}
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
