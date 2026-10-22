package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
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
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", h.login)
	mux.HandleFunc("GET /auth/callback", h.callback)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("GET /auth/me", h.me)
	mux.HandleFunc("POST /setup/claim-admin", h.claimAdmin)
}

func (h *Handler) claimAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var in struct {
		Issuer  string `json:"issuer"`
		Subject string `json:"subject"`
		Token   string `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	decodeErr := d.Decode(&in)
	if decodeErr != nil || requireEOF(d) != nil || in.Issuer == "" || in.Subject == "" || len(in.Token) < 32 || len(in.Token) > 512 {
		http.Error(w, "claim unavailable", http.StatusForbidden)
		return
	}
	u, _, _, err := h.current(r)
	if err != nil || u.Issuer != in.Issuer || u.Subject != in.Subject {
		http.Error(w, "claim unavailable", http.StatusForbidden)
		return
	}
	repo, ok := h.Repo.(BootstrapRepository)
	if !ok {
		http.Error(w, "claim unavailable", http.StatusForbidden)
		return
	}
	hash := sha256.Sum256([]byte(in.Token))
	if repo.ClaimAdmin(r.Context(), u.ID, u.Issuer, u.Subject, hash[:], time.Now()) != nil {
		http.Error(w, "claim unavailable", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func requireEOF(d *json.Decoder) error {
	var extra json.RawMessage
	if err := d.Decode(&extra); err != io.EOF {
		return err
	}
	return nil
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	preauth, err := oidc.RandomValue()
	if err != nil {
		http.Error(w, "auth unavailable", 500)
		return
	}
	state, err := oidc.RandomValue()
	if err != nil {
		http.Error(w, "auth unavailable", 500)
		return
	}
	nonce, err := oidc.RandomValue()
	if err != nil {
		http.Error(w, "auth unavailable", 500)
		return
	}
	verifier, err := oidc.RandomValue()
	if err != nil {
		http.Error(w, "auth unavailable", 500)
		return
	}
	if err = h.Repo.SaveTransaction(r.Context(), HashString(h.Secret, state), nonce, verifier, HashString(h.Secret, preauth), time.Now().Add(5*time.Minute)); err != nil {
		http.Error(w, "auth unavailable", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: preauthCookie, Value: preauth, Path: "/api/v1/auth", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.SetCookie(w, &http.Cookie{Name: "requestarr_oidc_state", Value: state, Path: "/api/v1/auth", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, h.Provider.AuthorizationURL(state, nonce, oidc.PKCEChallenge(verifier)), http.StatusFound)
}
func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("requestarr_oidc_state")
	if err != nil || !Equal(c.Value, r.URL.Query().Get("state")) {
		http.Error(w, "invalid login transaction", 400)
		return
	}
	preauth, err := r.Cookie(preauthCookie)
	if err != nil {
		http.Error(w, "invalid login transaction", 400)
		return
	}
	clearCookie(w, preauthCookie, "/api/v1/auth", h.Secure)
	clearCookie(w, "requestarr_oidc_state", "/api/v1/auth", h.Secure)
	nonce, verifier, err := h.Repo.ConsumeTransaction(r.Context(), HashString(h.Secret, c.Value), HashString(h.Secret, preauth.Value), time.Now())
	if err != nil {
		http.Error(w, "invalid login transaction", 400)
		return
	}
	if r.URL.Query().Get("error") != "" {
		http.Error(w, "identity provider rejected sign-in", 401)
		return
	}
	id, err := h.Provider.Exchange(r.Context(), r.URL.Query().Get("code"), verifier, nonce)
	if err != nil {
		http.Error(w, "sign-in failed", 401)
		return
	}
	uid, err := oidc.RandomValue()
	if err != nil {
		http.Error(w, "auth unavailable", 500)
		return
	}
	u, err := h.Repo.Login(r.Context(), h.Issuer, id.Subject, id.Email, id.DisplayName, uid, time.Now())
	if err != nil {
		http.Error(w, "sign-in failed", 500)
		return
	}
	token, err := oidc.RandomValue()
	if err != nil {
		http.Error(w, "auth unavailable", 500)
		return
	}
	csrf, err := oidc.RandomValue()
	if err != nil {
		http.Error(w, "auth unavailable", 500)
		return
	}
	exp := time.Now().Add(lifetime)
	if err = h.Repo.CreateSession(r.Context(), u.ID, Hash(h.Secret, token), Hash(h.Secret, csrf), exp); err != nil {
		http.Error(w, "auth unavailable", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteLaxMode, Expires: exp})
	http.SetCookie(w, &http.Cookie{Name: "requestarr_csrf", Value: csrf, Path: "/", Secure: h.Secure, SameSite: http.SameSiteLaxMode, Expires: exp})
	http.Redirect(w, r, h.PublicURL+"/", http.StatusSeeOther)
}
func (h *Handler) current(r *http.Request) (User, []byte, time.Time, error) {
	c, e := r.Cookie(sessionCookie)
	if e != nil {
		return User{}, nil, time.Time{}, e
	}
	return h.Repo.GetSession(r.Context(), Hash(h.Secret, c.Value), time.Now())
}

// CurrentSession reports whether the request carries a valid session.
func (h *Handler) CurrentSession(r *http.Request) (User, error) {
	u, _, _, err := h.current(r)
	return u, err
}
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u, csrf, exp, e := h.current(r)
	if e != nil {
		http.Error(w, "authentication required", 401)
		return
	}
	// The synchronizer token is returned only to the authenticated same-origin client.
	var token string
	if x, err := r.Cookie("requestarr_csrf"); err == nil && subtleHashEqual(Hash(h.Secret, x.Value), csrf) {
		token = x.Value
	}
	if token == "" {
		http.Error(w, "session invalid", 401)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Session-Expires", exp.UTC().Format(time.RFC3339))
	_ = json.NewEncoder(w).Encode(struct {
		User User   `json:"user"`
		CSRF string `json:"csrfToken"`
	}{u, token})
}
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r, h.PublicURL) {
		http.Error(w, "origin rejected", 403)
		return
	}
	u, csrf, _, err := h.current(r)
	_ = u
	if err != nil {
		http.Error(w, "authentication required", 401)
		return
	}
	c, _ := r.Cookie(sessionCookie)
	tc, err := r.Cookie("requestarr_csrf")
	if err != nil || !subtleHashEqual(Hash(h.Secret, tc.Value), csrf) || !Equal(tc.Value, r.Header.Get("X-CSRF-Token")) {
		http.Error(w, "CSRF validation failed", 403)
		return
	}
	if err = h.Repo.RevokeSession(r.Context(), Hash(h.Secret, c.Value), time.Now()); err != nil {
		http.Error(w, "logout failed", 500)
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
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			if !sameOrigin(r, h.PublicURL) {
				http.Error(w, "origin rejected", 403)
				return
			}
			_, csrf, _, _ := h.current(r)
			tc, e := r.Cookie("requestarr_csrf")
			if e != nil || !Equal(tc.Value, r.Header.Get("X-CSRF-Token")) || !subtleHashEqual(Hash(h.Secret, tc.Value), csrf) {
				http.Error(w, "CSRF validation failed", 403)
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
			http.Error(w, "authentication required", http.StatusUnauthorized)
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
