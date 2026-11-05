package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/auth/password"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
)

// Local sign-in and the forced password change (D040), through the whole
// router: who may reach what, for every role.

// localFake is a LocalStore with one credential for matrixUserID.
type localFake struct {
	hash       string
	mustChange bool
	changed    int
	events     []string
}

func (f *localFake) LocalCredentialByUsername(_ context.Context, name string) (auth.LocalCredential, error) {
	if name != "admin" {
		return auth.LocalCredential{}, auth.ErrNoCredential
	}
	return auth.LocalCredential{UserID: matrixUserID, Username: "admin", PasswordHash: f.hash, MustChange: f.mustChange}, nil
}
func (f *localFake) LocalCredentialByUserID(_ context.Context, id string) (auth.LocalCredential, error) {
	if id != matrixUserID {
		return auth.LocalCredential{}, auth.ErrNoCredential
	}
	return auth.LocalCredential{UserID: id, Username: "admin", PasswordHash: f.hash, MustChange: f.mustChange}, nil
}
func (f *localFake) ChangeLocalPassword(context.Context, string, string, []byte, time.Time) error {
	f.changed++
	return nil
}
func (f *localFake) RecordAuthEvent(_ context.Context, e auth.AuditEvent) error {
	f.events = append(f.events, e.Type)
	return nil
}
func (f *localFake) CreateLocalAdmin(context.Context, string, time.Time) (bool, error) {
	return false, nil
}

const localTestPassword = "correct horse battery staple"

func localRouter(t *testing.T, user auth.User, local *localFake) http.Handler {
	t.Helper()
	hasher := password.Hasher{Iterations: 1000}
	local.hash, _ = hasher.Hash(localTestPassword)
	backend := &matrixBackend{t: t}
	repo := matrixAuthRepo{csrfAuthRepo{statusAuthRepo{user: user}}, backend}
	return NewWithBackend(testLogger(), buildinfo.Info{}, backend, backend, backend, backend,
		&auth.Handler{Repo: repo, Local: local, Hasher: hasher, Secret: "test-secret", PublicURL: "http://example.test"})
}

func localCall(h http.Handler, method, path, body string, signedIn bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	if signedIn {
		r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	}
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", "csrf-token")
	r.Header.Set("Origin", "http://example.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLocalSignInIsReachableWithoutASessionAndGivesNothingAway(t *testing.T) {
	h := localRouter(t, auth.User{}, &localFake{})
	ok := localCall(h, "POST", "/auth/local/login", `{"username":"admin","password":"`+localTestPassword+`"}`, false)
	if ok.Code == http.StatusUnauthorized && strings.Contains(ok.Body.String(), "authentication_required") {
		t.Fatal("the sign-in route sits behind the session gate")
	}
	bad := localCall(h, "POST", "/auth/local/login", `{"username":"nobody","password":"whatever it is"}`, false)
	assertErrorEnvelope(t, bad, 401, "invalid_credentials")
}

func TestMustChangeSessionsCanOnlyChangeThePasswordSeeTheirUserOrSignOut(t *testing.T) {
	for _, role := range []string{"viewer", "requester", "admin"} {
		for _, tc := range []struct{ method, path, body string }{
			{"GET", "/requests", ""},
			{"GET", "/notifications", ""},
			{"GET", "/admin/users", ""},
			{"GET", "/admin/status", ""},
			{"GET", "/search/types", ""},
			{"GET", "/library", ""},
			{"POST", "/resolve", `{"url":"https://www.themoviedb.org/movie/11"}`},
			{"POST", "/requests", `{"catalogId":"` + matrixItemID + `"}`},
			{"PUT", "/admin/users/" + matrixItemID + "/role", `{"role":"viewer"}`},
			{"PUT", "/admin/secrets/tmdb_read_token", `{"value":"x"}`},
			{"POST", "/admin/sources/jellyfin/sync", ""},
			{"GET", "/does-not-exist", ""},
		} {
			t.Run(role+" "+tc.method+" "+tc.path, func(t *testing.T) {
				h := localRouter(t, auth.User{ID: matrixUserID, Role: role, MustChange: true}, &localFake{})
				w := localCall(h, tc.method, tc.path, tc.body, true)
				assertErrorEnvelope(t, w, 403, "password_change_required")
			})
		}
		t.Run(role+" may still read their user and sign out", func(t *testing.T) {
			h := localRouter(t, auth.User{ID: matrixUserID, Role: role, MustChange: true}, &localFake{})
			if w := localCall(h, "GET", "/auth/me", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"mustChangePassword":true`) {
				t.Fatalf("me: %d %s", w.Code, w.Body.String())
			}
			if w := localCall(h, "POST", "/auth/logout", "", true); w.Code != 204 {
				t.Fatalf("logout: %d %s", w.Code, w.Body.String())
			}
		})
		t.Run(role+" is not blocked once the password is changed", func(t *testing.T) {
			h := localRouter(t, auth.User{ID: matrixUserID, Role: role}, &localFake{})
			if w := localCall(h, "GET", "/notifications", "", true); w.Code != 200 {
				t.Fatalf("status=%d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestMustChangeDoesNotBlockPublicRoutes(t *testing.T) {
	h := localRouter(t, auth.User{ID: matrixUserID, Role: "admin", MustChange: true}, &localFake{})
	for _, p := range []string{"/version", "/setup/status"} {
		if w := localCall(h, "GET", p, "", true); w.Code != 200 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
}

func TestPasswordChangeRoutePerRole(t *testing.T) {
	body := `{"currentPassword":"` + localTestPassword + `","newPassword":"a brand new passphrase"}`
	t.Run("unauthenticated", func(t *testing.T) {
		local := &localFake{}
		w := localCall(localRouter(t, auth.User{}, local), "POST", "/auth/local/password", body, false)
		assertErrorEnvelope(t, w, 401, "authentication_required")
		if local.changed != 0 {
			t.Fatal("changed without a session")
		}
	})
	for _, role := range []string{"viewer", "requester"} {
		t.Run(role, func(t *testing.T) {
			local := &localFake{}
			// Another user than the one holding the credential.
			w := localCall(localRouter(t, auth.User{ID: "99999999-9999-4999-8999-999999999999", Role: role}, local), "POST", "/auth/local/password", body, true)
			assertErrorEnvelope(t, w, 403, "forbidden")
			if local.changed != 0 {
				t.Fatal("a person without a local password changed it")
			}
		})
	}
	t.Run("an OIDC administrator has no local password", func(t *testing.T) {
		local := &localFake{}
		w := localCall(localRouter(t, auth.User{ID: "99999999-9999-4999-8999-999999999999", Role: "admin"}, local), "POST", "/auth/local/password", body, true)
		assertErrorEnvelope(t, w, 403, "forbidden")
		if local.changed != 0 {
			t.Fatal("changed")
		}
	})
	t.Run("the local administrator, while must-change is set", func(t *testing.T) {
		local := &localFake{mustChange: true}
		w := localCall(localRouter(t, auth.User{ID: matrixUserID, Role: "admin", MustChange: true}, local), "POST", "/auth/local/password", body, true)
		if w.Code != 204 || local.changed != 1 {
			t.Fatalf("status=%d changed=%d body=%s", w.Code, local.changed, w.Body.String())
		}
	})
	t.Run("CSRF and origin are enforced", func(t *testing.T) {
		local := &localFake{}
		h := localRouter(t, auth.User{ID: matrixUserID, Role: "admin"}, local)
		for name, mod := range map[string]func(*http.Request){
			"no csrf":        func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
			"wrong csrf":     func(r *http.Request) { r.Header.Set("X-CSRF-Token", "x") },
			"foreign origin": func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") },
			"no origin":      func(r *http.Request) { r.Header.Del("Origin") },
		} {
			r := httptest.NewRequest("POST", "/api/v1/auth/local/password", strings.NewReader(body))
			r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
			r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", "csrf-token")
			r.Header.Set("Origin", "http://example.test")
			mod(r)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Errorf("%s: status=%d", name, w.Code)
			}
		}
		if local.changed != 0 {
			t.Fatal("a forged request changed the password")
		}
	})
}

func TestLocalAuthRoutesAreInTheRouteTable(t *testing.T) {
	_, rt := newRouter(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Secret: "test-secret"})
	got := map[string]routePolicy{}
	for _, p := range rt.Policies() {
		got[p.Pattern] = p
	}
	if p := got["POST /auth/local/login"]; p.Access != accessPublic || p.CSRF {
		t.Errorf("login policy %+v", p)
	}
	if p := got["POST /auth/local/password"]; p.Access != accessSession || !p.CSRF {
		t.Errorf("password policy %+v", p)
	}
}
