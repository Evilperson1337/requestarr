package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
)

const authTimeout = 10 * time.Second

type AuthRuntimeReader interface {
	VerifyActivated(context.Context) (auth.RuntimeConfig, error)
}
type AuthBrowser interface {
	Start(context.Context, auth.RuntimeConfig, string) (auth.LoginCookie, string, error)
	Complete(context.Context, auth.RuntimeConfig, auth.LoginCookie, string, string, string) (auth.SessionToken, auth.User, time.Time, error)
	Session(context.Context, auth.SessionToken) (auth.Principal, string, time.Time, error)
	Logout(context.Context, string) error
}
type AuthOptions struct {
	Runtime      AuthRuntimeReader
	Browser      AuthBrowser
	Cookies      *auth.LoginCookieCodec
	PublicOrigin string
	SecureCookie bool
}

func authRoutes(mux *http.ServeMux, options AuthOptions) {
	mux.Handle("GET /auth/login", noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		config, ok := activeAuth(r, options)
		if !ok {
			writeAPIError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		cookie, location, err := options.Browser.Start(r.Context(), config, options.PublicOrigin+"/auth/callback")
		if err != nil {
			writeAPIError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		sealed, err := options.Cookies.Seal(cookie)
		if err != nil {
			writeAPIError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is temporarily unavailable.")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: authLoginCookieName(options.SecureCookie), Value: sealed, Path: "/auth", Expires: cookie.ExpiresAt, MaxAge: int(auth.LoginTransactionLifetime / time.Second), Secure: options.SecureCookie, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, location, http.StatusFound)
	})))
	mux.Handle("GET /auth/callback", noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clearAuthLoginCookie(w, options.SecureCookie)
		config, ok := activeAuth(r, options)
		if !ok {
			http.Error(w, "Authentication unavailable.", http.StatusServiceUnavailable)
			return
		}
		values := r.URL.Query()
		if len(values["state"]) != 1 || len(values["code"]) != 1 || options.Cookies == nil {
			http.Error(w, "Authentication failed.", http.StatusBadRequest)
			return
		}
		raw, err := r.Cookie(authLoginCookieName(options.SecureCookie))
		if err != nil {
			http.Error(w, "Authentication failed.", http.StatusBadRequest)
			return
		}
		login, err := options.Cookies.Open(raw.Value)
		if err != nil {
			http.Error(w, "Authentication failed.", http.StatusBadRequest)
			return
		}
		token, _, expires, err := options.Browser.Complete(r.Context(), config, login, values.Get("state"), values.Get("code"), options.PublicOrigin+"/auth/callback")
		if err != nil {
			http.Error(w, "Authentication failed.", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: authSessionCookieName(options.SecureCookie), Value: string(token), Path: "/", Expires: expires, MaxAge: int(time.Until(expires).Seconds()), Secure: options.SecureCookie, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})))
	mux.Handle("GET /api/v1/auth/session", noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, _, expires, ok := authSession(r, options)
		if !ok {
			writeAPIError(w, r, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "userID": principal.UserID, "role": principal.Role, "expiresAt": expires})
	})))
	mux.Handle("POST /api/v1/auth/logout", noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Values("Origin")
		if len(origin) != 1 || origin[0] != options.PublicOrigin {
			writeAPIError(w, r, http.StatusForbidden, "origin_forbidden", "The request origin is not allowed.")
			return
		}
		_, id, _, ok := authSession(r, options)
		if !ok {
			writeAPIError(w, r, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			return
		}
		clearAuthSessionCookie(w, options.SecureCookie)
		if err := options.Browser.Logout(r.Context(), id); err != nil {
			writeAPIError(w, r, http.StatusServiceUnavailable, "logout_unavailable", "Logout could not be completed. Please try again.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))
}

func activeAuth(r *http.Request, o AuthOptions) (auth.RuntimeConfig, bool) {
	if o.Runtime == nil || o.Browser == nil || o.Cookies == nil {
		return auth.RuntimeConfig{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), authTimeout)
	defer cancel()
	c, e := o.Runtime.VerifyActivated(ctx)
	return c, e == nil
}
func authSession(r *http.Request, o AuthOptions) (auth.Principal, string, time.Time, bool) {
	result := readAuthSession(r, o)
	if result.state != authSessionAuthenticated {
		return auth.Principal{}, "", time.Time{}, false
	}
	return result.principal, result.sessionID, result.expiresAt, true
}

type authSessionState uint8

const (
	authSessionUnavailable authSessionState = iota
	authSessionUnauthenticated
	authSessionAuthenticated
)

type authSessionResult struct {
	state     authSessionState
	principal auth.Principal
	sessionID string
	expiresAt time.Time
}

// readAuthSession distinguishes unavailable authentication infrastructure from
// a missing or invalid browser session. It deliberately does not disclose why
// an unauthenticated session lookup failed.
func readAuthSession(r *http.Request, o AuthOptions) authSessionResult {
	if o.Runtime == nil || o.Browser == nil || o.Cookies == nil {
		return authSessionResult{state: authSessionUnavailable}
	}
	ctx, cancel := context.WithTimeout(r.Context(), authTimeout)
	defer cancel()
	if _, err := o.Runtime.VerifyActivated(ctx); err != nil {
		return authSessionResult{state: authSessionUnavailable}
	}
	cookie, err := r.Cookie(authSessionCookieName(o.SecureCookie))
	if err != nil {
		return authSessionResult{state: authSessionUnauthenticated}
	}
	token, err := auth.ParseSessionToken(cookie.Value)
	if err != nil {
		return authSessionResult{state: authSessionUnauthenticated}
	}
	principal, sessionID, expiresAt, err := o.Browser.Session(ctx, token)
	if err != nil || !principal.Valid() || sessionID == "" || expiresAt.IsZero() {
		return authSessionResult{state: authSessionUnauthenticated}
	}
	return authSessionResult{state: authSessionAuthenticated, principal: principal, sessionID: sessionID, expiresAt: expiresAt}
}
func authLoginCookieName(secure bool) string {
	if secure {
		// __Host cookies are required to use Path=/; this short-lived callback
		// cookie is intentionally scoped to /auth instead.
		return "__Secure-requestarr_login"
	}
	return "requestarr_login_dev"
}
func authSessionCookieName(secure bool) string {
	if secure {
		return "__Host-requestarr_session"
	}
	return "requestarr_session_dev"
}
func clearAuthLoginCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: authLoginCookieName(secure), Value: "", Path: "/auth", MaxAge: -1, Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}
func clearAuthSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: authSessionCookieName(secure), Value: "", Path: "/", MaxAge: -1, Secure: secure, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

var _ = errors.Is
