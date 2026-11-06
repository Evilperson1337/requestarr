package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/evilperson1337/requestarr/internal/apierror"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/authsettings"
)

const sessionCookie = "requestarr_session"
const preauthCookie = "requestarr_oidc_preauth"
const lifetime = 12 * time.Hour

type Handler struct {
	Repo     Repository
	Provider oidc.Provider
	// OIDC returns the live immutable snapshot; static Provider is retained for embedders.
	OIDC                      func() *authsettings.Snapshot
	pendingMu                 sync.Mutex
	pending                   map[string]pendingOIDC
	Issuer, PublicURL, Secret string
	Secure                    bool

	// The local administrator (D040). Local may be nil, which turns local
	// sign-in off. Hasher, Clock, Throttle and Logger default sensibly.
	Local    LocalStore
	Hasher   PasswordHasher
	Clock    func() time.Time
	Throttle *Throttle
	Logger   *slog.Logger
	// TrustedProxies are the reverse proxies whose X-Forwarded-For is believed
	// when finding the client address. Empty means never (D040).
	TrustedProxies []netip.Prefix
	// Sleep holds a failed sign-in response; nil uses a real timer.
	Sleep func(ctx context.Context, d time.Duration)

	throttleOnce, dummyOnce sync.Once
	dummy                   string
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", h.login)
	mux.HandleFunc("GET /auth/callback", h.callback)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("GET /auth/me", h.me)
	mux.HandleFunc("POST /auth/local/login", h.localLogin)
	mux.Handle("POST /auth/local/password", h.CSRFMiddleware(http.HandlerFunc(h.changePassword)))
}

func requireEOF(d *json.Decoder) error {
	var extra json.RawMessage
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	snap := &authsettings.Snapshot{Provider: h.Provider, Issuer: h.Issuer}
	if h.OIDC != nil {
		snap = h.OIDC()
	}
	if snap == nil || snap.Provider == nil {
		apierror.Write(w, r, 503, "oidc_unavailable", "OIDC sign-in is not available; use administrator sign-in")
		return
	}

	preauth, err := oidc.RandomValue()
	if err != nil {
		apierror.Write(w, r, 500, "auth_unavailable", "auth unavailable")
		return
	}
	state, err := oidc.RandomValue()
	if err != nil {
		apierror.Write(w, r, 500, "auth_unavailable", "auth unavailable")
		return
	}
	nonce, err := oidc.RandomValue()
	if err != nil {
		apierror.Write(w, r, 500, "auth_unavailable", "auth unavailable")
		return
	}
	verifier, err := oidc.RandomValue()
	if err != nil {
		apierror.Write(w, r, 500, "auth_unavailable", "auth unavailable")
		return
	}
	if err = h.Repo.SaveTransaction(r.Context(), HashString(h.Secret, state), nonce, verifier, HashString(h.Secret, preauth), h.now().Add(5*time.Minute)); err != nil {
		apierror.Write(w, r, 500, "auth_unavailable", "auth unavailable")
		return
	}
	if !h.rememberOIDC(HashString(h.Secret, state), snap) {
		apierror.Write(w, r, 503, "auth_unavailable", "too many pending sign-ins; try again shortly")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: preauthCookie, Value: preauth, Path: "/api/v1/auth", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.SetCookie(w, &http.Cookie{Name: "requestarr_oidc_state", Value: state, Path: "/api/v1/auth", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, snap.Provider.AuthorizationURL(state, nonce, oidc.PKCEChallenge(verifier)), http.StatusFound)
}
func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("requestarr_oidc_state")
	if err != nil || !Equal(c.Value, r.URL.Query().Get("state")) {
		apierror.Write(w, r, 400, "invalid_login_transaction", "invalid login transaction")
		return
	}
	preauth, err := r.Cookie(preauthCookie)
	if err != nil {
		apierror.Write(w, r, 400, "invalid_login_transaction", "invalid login transaction")
		return
	}
	clearCookie(w, preauthCookie, "/api/v1/auth", h.Secure)
	clearCookie(w, "requestarr_oidc_state", "/api/v1/auth", h.Secure)
	nonce, verifier, err := h.Repo.ConsumeTransaction(r.Context(), HashString(h.Secret, c.Value), HashString(h.Secret, preauth.Value), h.now())
	if err != nil {
		apierror.Write(w, r, 400, "invalid_login_transaction", "invalid login transaction")
		return
	}
	snap := h.takeOIDC(HashString(h.Secret, c.Value))
	if snap == nil {
		apierror.Write(w, r, 400, "invalid_login_transaction", "login expired; start sign-in again")
		return
	}
	if r.URL.Query().Get("error") != "" {
		apierror.Write(w, r, 401, "sign_in_rejected", "identity provider rejected sign-in")
		return
	}
	id, err := snap.Provider.Exchange(r.Context(), r.URL.Query().Get("code"), verifier, nonce)
	if err != nil {
		apierror.Write(w, r, 401, "sign_in_failed", "sign-in failed")
		return
	}
	uid, err := oidc.RandomUUID()
	if err != nil {
		apierror.Write(w, r, 500, "auth_unavailable", "auth unavailable")
		return
	}
	u, err := h.Repo.Login(r.Context(), snap.Issuer, id.Subject, id.Email, id.DisplayName, uid, h.now())
	if err != nil {
		apierror.Write(w, r, 500, "sign_in_failed", "sign-in failed")
		return
	}
	if err = h.issueSession(w, r, u.ID); err != nil {
		apierror.Write(w, r, 500, "auth_unavailable", "auth unavailable")
		return
	}
	http.Redirect(w, r, h.PublicURL+"/", http.StatusSeeOther)
}

// issueSession creates a session for the user and sets its cookies.
func (h *Handler) issueSession(w http.ResponseWriter, r *http.Request, userID string) error {
	return h.issueSessionUsing(w, r, userID, h.Repo.CreateSession)
}

func (h *Handler) issueSessionUsing(w http.ResponseWriter, r *http.Request, userID string, create func(context.Context, string, []byte, []byte, time.Time) error) error {
	token, err := oidc.RandomValue()
	if err != nil {
		return err
	}
	csrf, err := oidc.RandomValue()
	if err != nil {
		return err
	}
	exp := h.now().Add(lifetime)
	if err = create(r.Context(), userID, Hash(h.Secret, token), Hash(h.Secret, csrf), exp); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteLaxMode, Expires: exp})
	http.SetCookie(w, &http.Cookie{Name: "requestarr_csrf", Value: csrf, Path: "/", Secure: h.Secure, SameSite: http.SameSiteLaxMode, Expires: exp})
	return nil
}
func (h *Handler) current(r *http.Request) (User, []byte, time.Time, error) {
	c, e := r.Cookie(sessionCookie)
	if e != nil {
		return User{}, nil, time.Time{}, e
	}
	return h.Repo.GetSession(r.Context(), Hash(h.Secret, c.Value), h.now())
}

// CurrentSession reports whether the request carries a valid session.
func (h *Handler) CurrentSession(r *http.Request) (User, error) {
	u, _, _, err := h.current(r)
	return u, err
}
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u, csrf, exp, e := h.current(r)
	if e != nil {
		apierror.Write(w, r, 401, "authentication_required", "authentication required")
		return
	}
	// The synchronizer token is returned only to the authenticated same-origin client.
	var token string
	if x, err := r.Cookie("requestarr_csrf"); err == nil && subtleHashEqual(Hash(h.Secret, x.Value), csrf) {
		token = x.Value
	}
	if token == "" {
		apierror.Write(w, r, 401, "session_invalid", "session invalid")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Session-Expires", exp.UTC().Format(time.RFC3339))
	_ = json.NewEncoder(w).Encode(struct {
		User       User   `json:"user"`
		CSRF       string `json:"csrfToken"`
		MustChange bool   `json:"mustChangePassword"`
	}{u, token, u.MustChange})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r, h.PublicURL) {
		apierror.Write(w, r, 403, "origin_rejected", "origin rejected")
		return
	}
	u, csrf, _, err := h.current(r)
	_ = u
	if err != nil {
		apierror.Write(w, r, 401, "authentication_required", "authentication required")
		return
	}
	c, _ := r.Cookie(sessionCookie)
	tc, err := r.Cookie("requestarr_csrf")
	if err != nil || !subtleHashEqual(Hash(h.Secret, tc.Value), csrf) || !Equal(tc.Value, r.Header.Get("X-CSRF-Token")) {
		apierror.Write(w, r, 403, "csrf_invalid", "CSRF validation failed")
		return
	}
	if err = h.Repo.RevokeSession(r.Context(), Hash(h.Secret, c.Value), h.now()); err != nil {
		apierror.Write(w, r, 500, "logout_failed", "logout failed")
		return
	}
	clearCookie(w, sessionCookie, "/", h.Secure)
	clearCookie(w, "requestarr_csrf", "/", h.Secure)
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unsafeMethod(r.Method) {
			if _, _, _, err := h.current(r); err != nil {
				apierror.Write(w, r, http.StatusUnauthorized, "authentication_required", "authentication required")
				return
			}
			if !sameOrigin(r, h.PublicURL) {
				apierror.Write(w, r, 403, "origin_rejected", "origin rejected")
				return
			}
			_, csrf, _, _ := h.current(r)
			tc, e := r.Cookie("requestarr_csrf")
			if e != nil || !Equal(tc.Value, r.Header.Get("X-CSRF-Token")) || !subtleHashEqual(Hash(h.Secret, tc.Value), csrf) {
				apierror.Write(w, r, 403, "csrf_invalid", "CSRF validation failed")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func unsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func (h *Handler) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/login" || r.URL.Path == "/auth/callback" || r.URL.Path == "/auth/me" || r.URL.Path == "/auth/logout" {
			next.ServeHTTP(w, r)
			return
		}
		if _, _, _, err := h.current(r); err != nil {
			apierror.Write(w, r, http.StatusUnauthorized, "authentication_required", "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func sameOrigin(r *http.Request, public string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	a, e := url.Parse(origin)
	if e != nil {
		return false
	}
	b, e := url.Parse(public)
	return e == nil && a.User == nil && a.Path == "" && a.RawQuery == "" && a.Fragment == "" && strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}
func subtleHashEqual(a, b []byte) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare(a, b) == 1
}
func clearCookie(w http.ResponseWriter, name, path string, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: path, HttpOnly: name != "requestarr_csrf", Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

// Pending snapshots have the same lifetime as the browser-bound transaction.
// Bounded storage prevents abandoned sign-ins from retaining providers forever.
type pendingOIDC struct {
	snapshot *authsettings.Snapshot
	expires  time.Time
}

func (h *Handler) rememberOIDC(key string, snapshot *authsettings.Snapshot) bool {
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()
	now := h.now()
	if h.pending == nil {
		h.pending = map[string]pendingOIDC{}
	}
	for key, p := range h.pending {
		if !p.expires.After(now) {
			delete(h.pending, key)
		}
	}
	if len(h.pending) >= 4096 {
		return false
	}
	h.pending[key] = pendingOIDC{snapshot: snapshot, expires: now.Add(5 * time.Minute)}
	return true
}
func (h *Handler) takeOIDC(key string) *authsettings.Snapshot {
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()
	p := h.pending[key]
	delete(h.pending, key)
	if !p.expires.After(h.now()) {
		return nil
	}
	return p.snapshot
}
