package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/evilperson1337/requestarr/internal/apierror"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth/oidc"
)

const sessionCookie = "requestarr_session"
const preauthCookie = "requestarr_oidc_preauth"
const lifetime = 12 * time.Hour

type Handler struct {
	Repo                      Repository
	Provider                  oidc.Provider
	Issuer, PublicURL, Secret string
	Secure                    bool
	Setup                     SetupState

	// The local administrator (D040). Local may be nil, which turns local
	// sign-in off. Hasher, Clock, Throttle and Logger default sensibly.
	Local    LocalStore
	Hasher   PasswordHasher
	Clock    func() time.Time
	Throttle *Throttle
	Logger   *slog.Logger

	throttleOnce, dummyOnce sync.Once
	dummy                   string
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", h.login)
	mux.HandleFunc("GET /auth/callback", h.callback)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("GET /auth/me", h.me)
	mux.HandleFunc("POST /setup/claim-admin", h.claimAdmin)
	mux.HandleFunc("POST /auth/local/login", h.localLogin)
	mux.Handle("POST /auth/local/password", h.CSRFMiddleware(http.HandlerFunc(h.changePassword)))
}

func (h *Handler) claimAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var in struct {
		Issuer  *string `json:"issuer"`
		Subject *string `json:"subject"`
		Token   string  `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	decodeErr := d.Decode(&in)
	if decodeErr != nil || requireEOF(d) != nil || len(in.Token) < 32 || len(in.Token) > 512 {
		apierror.Write(w, r, http.StatusForbidden, "claim_unavailable", "claim unavailable")
		return
	}
	u, _, _, err := h.current(r)
	if err != nil || (in.Issuer != nil && *in.Issuer != u.Issuer) || (in.Subject != nil && *in.Subject != u.Subject) {
		apierror.Write(w, r, http.StatusForbidden, "claim_unavailable", "claim unavailable")
		return
	}
	repo, ok := h.Repo.(BootstrapRepository)
	if !ok {
		apierror.Write(w, r, http.StatusForbidden, "claim_unavailable", "claim unavailable")
		return
	}
	hash := sha256.Sum256([]byte(in.Token))
	if repo.ClaimAdmin(r.Context(), u.ID, u.Issuer, u.Subject, hash[:], h.now()) != nil {
		apierror.Write(w, r, http.StatusForbidden, "claim_unavailable", "claim unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	http.SetCookie(w, &http.Cookie{Name: preauthCookie, Value: preauth, Path: "/api/v1/auth", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.SetCookie(w, &http.Cookie{Name: "requestarr_oidc_state", Value: state, Path: "/api/v1/auth", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, h.Provider.AuthorizationURL(state, nonce, oidc.PKCEChallenge(verifier)), http.StatusFound)
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
	if r.URL.Query().Get("error") != "" {
		apierror.Write(w, r, 401, "sign_in_rejected", "identity provider rejected sign-in")
		return
	}
	id, err := h.Provider.Exchange(r.Context(), r.URL.Query().Get("code"), verifier, nonce)
	if err != nil {
		apierror.Write(w, r, 401, "sign_in_failed", "sign-in failed")
		return
	}
	uid, err := oidc.RandomUUID()
	if err != nil {
		apierror.Write(w, r, 500, "auth_unavailable", "auth unavailable")
		return
	}
	u, err := h.Repo.Login(r.Context(), h.Issuer, id.Subject, id.Email, id.DisplayName, uid, h.now())
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
	token, err := oidc.RandomValue()
	if err != nil {
		return err
	}
	csrf, err := oidc.RandomValue()
	if err != nil {
		return err
	}
	exp := h.now().Add(lifetime)
	if err = h.Repo.CreateSession(r.Context(), userID, Hash(h.Secret, token), Hash(h.Secret, csrf), exp); err != nil {
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
		User            User             `json:"user"`
		CSRF            string           `json:"csrfToken"`
		FirstAdminClaim *FirstAdminClaim `json:"firstAdminClaim,omitempty"`
		MustChange      bool             `json:"mustChangePassword"`
	}{u, token, h.firstAdminClaim(r.Context(), u), u.MustChange})
}

// FirstAdminClaim is the caller's own verified identity, shown only to that
// caller while first-admin setup is unclaimed (D037). It is what the operator
// types into setup-token; it grants nothing without the shell-issued token.
type FirstAdminClaim struct {
	Issuer             string `json:"issuer"`
	Subject            string `json:"subject"`
	IssuerIsConfigured bool   `json:"issuerIsConfigured"`
}

// firstAdminClaim returns nil unless setup is known to be unclaimed; a failed
// lookup exposes nothing.
func (h *Handler) firstAdminClaim(ctx context.Context, u User) *FirstAdminClaim {
	if h.Setup == nil {
		return nil
	}
	complete, err := h.Setup.SetupComplete(ctx)
	if err != nil || complete {
		return nil
	}
	return &FirstAdminClaim{Issuer: u.Issuer, Subject: u.Subject, IssuerIsConfigured: u.Issuer == h.Issuer}
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
