package auth

import (
	"context"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type snapshotRepo struct {
	authRepoFake
	issuer string
}

func (r *snapshotRepo) Login(_ context.Context, issuer, _, _, _, uid string, _ time.Time) (User, error) {
	r.issuer = issuer
	return User{ID: uid}, nil
}
func TestInFlightSignInKeepsProviderAndIssuerSnapshot(t *testing.T) {
	var current atomic.Pointer[authsettings.Snapshot]
	current.Store(&authsettings.Snapshot{Provider: identityProviderFake{}, Issuer: "https://original.test"})
	repo := &snapshotRepo{}
	h := &Handler{Repo: repo, OIDC: current.Load, Secret: "secret", PublicURL: "https://app.example"}
	mux := http.NewServeMux()
	h.Register(mux)
	login := httptest.NewRecorder()
	mux.ServeHTTP(login, httptest.NewRequest("GET", "/auth/login", nil))
	if login.Code != 302 {
		t.Fatal("login should start")
	}
	current.Store(&authsettings.Snapshot{}) // disable does not interrupt this flow
	var state string
	for _, c := range login.Result().Cookies() {
		if c.Name == "requestarr_oidc_state" {
			state = c.Value
		}
	}
	req := httptest.NewRequest("GET", "/auth/callback?code=c&state="+state, nil)
	for _, c := range login.Result().Cookies() {
		req.AddCookie(c)
	}
	out := httptest.NewRecorder()
	mux.ServeHTTP(out, req)
	if out.Code != 303 || repo.issuer != "https://original.test" {
		t.Fatal("callback lost original provider or issuer")
	}
	next := httptest.NewRecorder()
	mux.ServeHTTP(next, httptest.NewRequest("GET", "/auth/login", nil))
	if next.Code != 503 {
		t.Fatal("new sign-ins must respect disable")
	}
	replay := httptest.NewRecorder()
	mux.ServeHTTP(replay, req)
	if replay.Code != 400 {
		t.Fatal("callback replay must fail")
	}
}
