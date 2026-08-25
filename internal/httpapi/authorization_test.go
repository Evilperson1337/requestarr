package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
)

func TestRequireAdmin(t *testing.T) {
	tests := []struct {
		name       string
		runtimeErr error
		cookie     string
		principal  auth.Principal
		sessionErr error
		want       int
		called     bool
	}{
		{name: "runtime unavailable", runtimeErr: auth.ErrRuntimeConfigUnavailable, want: http.StatusServiceUnavailable},
		{name: "missing cookie", want: http.StatusUnauthorized},
		{name: "malformed cookie", cookie: "bad", want: http.StatusUnauthorized},
		{name: "session unavailable", cookie: authTestSessionToken, sessionErr: auth.ErrSessionRequired, want: http.StatusUnauthorized},
		{name: "invalid principal", cookie: authTestSessionToken, principal: auth.Principal{UserID: "user", Role: "invalid"}, want: http.StatusUnauthorized},
		{name: "viewer", cookie: authTestSessionToken, principal: auth.Principal{UserID: "user", Role: auth.RoleViewer}, want: http.StatusForbidden},
		{name: "requester", cookie: authTestSessionToken, principal: auth.Principal{UserID: "user", Role: auth.RoleRequester}, want: http.StatusForbidden},
		{name: "admin", cookie: authTestSessionToken, principal: auth.Principal{UserID: "user", Role: auth.RoleAdmin}, want: http.StatusNoContent, called: true},
	}
	config := auth.RuntimeConfig{AuthenticationRevision: 1, Issuer: "https://issuer.example", ClientID: "requestarr", ClientType: "public", Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: auth.RoleViewer, ActivatedAt: time.Unix(1, 0)}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			browser := &adminBrowserFake{principal: tt.principal, err: tt.sessionErr}
			called := false
			handler := requireAdmin(AuthOptions{Runtime: authRuntimeFake{config: config, err: tt.runtimeErr}, Browser: browser, Cookies: testLoginCodec(t)}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: authSessionCookieName(false), Value: tt.cookie})
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tt.want || called != tt.called {
				t.Fatalf("status=%d called=%t", recorder.Code, called)
			}
		})
	}
}

func TestRequireAdminChecksRuntimeAndSessionOnce(t *testing.T) {
	config := auth.RuntimeConfig{AuthenticationRevision: 1, Issuer: "https://issuer.example", ClientID: "requestarr", ClientType: "public", Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: auth.RoleViewer, ActivatedAt: time.Unix(1, 0)}
	runtime := &countingRuntimeFake{config: config}
	browser := &adminBrowserFake{principal: auth.Principal{UserID: "admin", Role: auth.RoleAdmin}}
	handler := requireAdmin(AuthOptions{Runtime: runtime, Browser: browser, Cookies: testLoginCodec(t)}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: authSessionCookieName(false), Value: authTestSessionToken})
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if runtime.calls != 1 || browser.calls != 1 {
		t.Fatalf("runtime calls=%d browser calls=%d", runtime.calls, browser.calls)
	}
}

func testLoginCodec(t *testing.T) *auth.LoginCookieCodec {
	t.Helper()
	var key [32]byte
	codec, err := auth.NewLoginCookieCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

type adminBrowserFake struct {
	principal auth.Principal
	err       error
	calls     int
}

func (f *adminBrowserFake) Start(context.Context, auth.RuntimeConfig, string) (auth.LoginCookie, string, error) {
	return auth.LoginCookie{}, "", auth.ErrLoginUnavailable
}
func (f *adminBrowserFake) Complete(context.Context, auth.RuntimeConfig, auth.LoginCookie, string, string, string) (auth.SessionToken, auth.User, time.Time, error) {
	return "", auth.User{}, time.Time{}, auth.ErrLoginInvalid
}
func (f *adminBrowserFake) Session(context.Context, auth.SessionToken) (auth.Principal, string, time.Time, error) {
	f.calls++
	return f.principal, "session-id", time.Now().Add(time.Hour), f.err
}
func (f *adminBrowserFake) Logout(context.Context, string) error { return nil }

type countingRuntimeFake struct {
	config auth.RuntimeConfig
	calls  int
}

func (f *countingRuntimeFake) VerifyActivated(context.Context) (auth.RuntimeConfig, error) {
	f.calls++
	return f.config, nil
}
