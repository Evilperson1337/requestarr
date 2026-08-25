package httpapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
)

type authRuntimeFake struct {
	config auth.RuntimeConfig
	err    error
}

func (f authRuntimeFake) VerifyActivated(context.Context) (auth.RuntimeConfig, error) {
	return f.config, f.err
}

type authBrowserFake struct{}

func (authBrowserFake) Start(context.Context, auth.RuntimeConfig, string) (auth.LoginCookie, string, error) {
	return auth.LoginCookie{}, "", auth.ErrLoginUnavailable
}
func (authBrowserFake) Complete(context.Context, auth.RuntimeConfig, auth.LoginCookie, string, string, string) (auth.SessionToken, auth.User, time.Time, error) {
	return "", auth.User{}, time.Time{}, auth.ErrLoginInvalid
}
func (authBrowserFake) Session(context.Context, auth.SessionToken) (auth.Principal, string, time.Time, error) {
	return auth.Principal{}, "", time.Time{}, auth.ErrSessionRequired
}
func (authBrowserFake) Logout(context.Context, string) error { return nil }

func TestAuthRoutesFailClosedWhenRuntimeInactive(t *testing.T) {
	var key [32]byte
	codec, _ := auth.NewLoginCookieCodec(key)
	mux := http.NewServeMux()
	authRoutes(mux, AuthOptions{Runtime: authRuntimeFake{err: auth.ErrRuntimeConfigNotActivated}, Browser: authBrowserFake{}, Cookies: codec, PublicOrigin: "https://requestarr.example", SecureCookie: true})
	for _, request := range []*http.Request{httptest.NewRequest(http.MethodGet, "/auth/login", nil), httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable && recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status=%d", request.URL.Path, recorder.Code)
		}
	}
}

func TestLogoutRejectsCrossOrigin(t *testing.T) {
	mux := http.NewServeMux()
	authRoutes(mux, AuthOptions{PublicOrigin: "https://requestarr.example"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.Header.Set("Origin", "https://evil.example")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d", recorder.Code)
	}
}

func TestAuthRoutesCompleteBrowserCallbackAndLogout(t *testing.T) {
	config := auth.RuntimeConfig{AuthenticationRevision: 1, Issuer: "https://issuer.example", ClientID: "requestarr", ClientType: "public", Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: auth.RoleViewer, ActivatedAt: time.Unix(1, 0)}
	browser := &authBrowserFlowFake{}
	var key [32]byte
	codec, err := auth.NewLoginCookieCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	authRoutes(mux, AuthOptions{Runtime: authRuntimeFake{config: config}, Browser: browser, Cookies: codec, PublicOrigin: server.URL})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	login, err := client.Get(server.URL + "/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	if login.StatusCode != http.StatusFound || login.Header.Get("Location") != "https://provider.example/authorize" {
		t.Fatalf("login status=%d location=%q", login.StatusCode, login.Header.Get("Location"))
	}
	loginCookies := login.Cookies()
	if len(loginCookies) != 1 {
		t.Fatalf("login cookies=%+v set-cookie=%q", loginCookies, login.Header.Values("Set-Cookie"))
	}
	if _, err := codec.Open(loginCookies[0].Value); err != nil {
		t.Fatalf("opened login cookie error=%v cookie=%+v", err, loginCookies[0])
	}
	callbackRequest, err := http.NewRequest(http.MethodGet, server.URL+"/auth/callback?state="+authTestState+"&code=code", nil)
	if err != nil {
		t.Fatal(err)
	}
	callbackRequest.AddCookie(loginCookies[0])
	callback, err := client.Do(callbackRequest)
	if err != nil {
		t.Fatal(err)
	}
	if callback.StatusCode != http.StatusSeeOther || callback.Header.Get("Location") != "/" || browser.completed != 1 {
		t.Fatalf("callback status=%d location=%q completed=%d body=%q", callback.StatusCode, callback.Header.Get("Location"), browser.completed, readResponseBody(t, callback))
	}
	session, err := client.Get(server.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	if session.StatusCode != http.StatusOK {
		t.Fatalf("session status=%d", session.StatusCode)
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/logout", nil)
	request.Header.Set("Origin", server.URL)
	logout, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if logout.StatusCode != http.StatusNoContent || browser.logoutID != "session-1" {
		t.Fatalf("logout status=%d id=%q", logout.StatusCode, browser.logoutID)
	}
}

func TestLogoutClearsLocalCookieButReportsDurableRevocationFailure(t *testing.T) {
	config := auth.RuntimeConfig{AuthenticationRevision: 1, Issuer: "https://issuer.example", ClientID: "requestarr", ClientType: "public", Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: auth.RoleViewer, ActivatedAt: time.Unix(1, 0)}
	browser := &authBrowserFlowFake{logoutErr: auth.ErrSessionRequired}
	var key [32]byte
	codec, err := auth.NewLoginCookieCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	authRoutes(mux, AuthOptions{Runtime: authRuntimeFake{config: config}, Browser: browser, Cookies: codec, PublicOrigin: server.URL})
	req := httptest.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/logout", nil)
	req.Header.Set("Origin", server.URL)
	req.AddCookie(&http.Cookie{Name: authSessionCookieName(false), Value: authTestSessionToken})
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable || browser.logoutID != "session-1" {
		t.Fatalf("status=%d id=%q", recorder.Code, browser.logoutID)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == authSessionCookieName(false) && cookie.MaxAge < 0 {
			return
		}
	}
	t.Fatal("session cookie was not cleared")
}

func readResponseBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	var body [1024]byte
	n, _ := response.Body.Read(body[:])
	return string(body[:n])
}

type authBrowserFlowFake struct {
	completed int
	logoutID  string
	logoutErr error
}

func (f *authBrowserFlowFake) Start(_ context.Context, _ auth.RuntimeConfig, _ string) (auth.LoginCookie, string, error) {
	state, _ := auth.ParseState(authTestState)
	nonce, _ := auth.ParseNonce(authTestNonce)
	verifier, _ := auth.ParsePKCEVerifier(authTestVerifier)
	return auth.LoginCookie{State: state, Nonce: nonce, PKCEVerifier: verifier, ExpiresAt: time.Now().Add(time.Minute)}, "https://provider.example/authorize", nil
}
func (f *authBrowserFlowFake) Complete(_ context.Context, _ auth.RuntimeConfig, cookie auth.LoginCookie, state, code, _ string) (auth.SessionToken, auth.User, time.Time, error) {
	if state != string(cookie.State) || code != "code" {
		return "", auth.User{}, time.Time{}, auth.ErrLoginInvalid
	}
	f.completed++
	return auth.SessionToken(authTestSessionToken), auth.User{ID: "user-1", Role: auth.RoleViewer}, time.Now().Add(time.Hour), nil
}
func (f *authBrowserFlowFake) Session(_ context.Context, token auth.SessionToken) (auth.Principal, string, time.Time, error) {
	if string(token) != authTestSessionToken {
		return auth.Principal{}, "", time.Time{}, auth.ErrSessionRequired
	}
	return auth.Principal{UserID: "user-1", Role: auth.RoleViewer}, "session-1", time.Now().Add(time.Hour), nil
}
func (f *authBrowserFlowFake) Logout(_ context.Context, id string) error {
	f.logoutID = id
	return f.logoutErr
}

var (
	authTestState        = authTestOpaque(0)
	authTestNonce        = authTestOpaque(1)
	authTestSessionToken = authTestOpaque(2)
	// PKCE accepts URL-safe opaque characters, independently of state/nonce encoding.
	authTestVerifier = "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
)

func authTestOpaque(value byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = value
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
