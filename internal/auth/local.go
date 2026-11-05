package auth

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth/password"
)

// The built-in local administrator (D040): one account, signed in with a
// password, the only one in the product.

// LocalUsername is the built-in account's username.
const LocalUsername = "admin"

// Password length limits. There are no composition rules.
const (
	MinPasswordLength = password.MinLength
	MaxPasswordLength = password.MaxLength
)

// Audit event types written for the local account.
const (
	AuditSignInSuccess         = "signin_success"
	AuditSignInFailure         = "signin_failure"
	AuditSignInThrottled       = "signin_throttled"
	AuditPasswordChanged       = "password_changed"
	AuditPasswordChangeFailure = "password_change_failure"
	AuditAdminCreated          = "admin_created"
)

// ErrNoCredential means no local credential matches.
var ErrNoCredential = errors.New("no local credential")

// LocalCredential is the local administrator's stored password. PasswordHash
// is the encoded hash and never leaves the server.
type LocalCredential struct {
	UserID       string
	Username     string
	PasswordHash string
	MustChange   bool
	Disabled     bool
}

// AuditEvent is one row of the authentication audit. It never carries
// password material, and Username is set only when it names a real account.
type AuditEvent struct {
	Type       string
	UserID     string
	Username   string
	RemoteAddr string
	At         time.Time
}

// LocalStore is the storage the local account needs.
type LocalStore interface {
	LocalCredentialByUsername(ctx context.Context, username string) (LocalCredential, error)
	LocalCredentialByUserID(ctx context.Context, userID string) (LocalCredential, error)
	// ChangeLocalPassword stores the new hash, clears must-change and ends
	// every session of the user except the one with token hash keep, together.
	ChangeLocalPassword(ctx context.Context, userID, hash string, keep []byte, now time.Time) error
	RecordAuthEvent(ctx context.Context, e AuditEvent) error
	// CreateLocalAdmin creates the built-in administrator with must-change
	// set, but only while there are no users; it reports whether it did.
	CreateLocalAdmin(ctx context.Context, hash string, now time.Time) (bool, error)
}

// PasswordHasher is the narrow hashing interface; password.Hasher implements it.
type PasswordHasher interface {
	Hash(pw string) (string, error)
	Verify(pw, encoded string) bool
	NeedsRehash(encoded string) bool
}

func (h *Handler) hasher() PasswordHasher {
	if h.Hasher != nil {
		return h.Hasher
	}
	return password.Default
}

func (h *Handler) now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now()
}

func (h *Handler) throttle() *Throttle {
	h.throttleOnce.Do(func() {
		if h.Throttle == nil {
			h.Throttle = NewThrottle(h.now)
		}
	})
	return h.Throttle
}

// dummyHash is verified against when the username is unknown, so an unknown
// name costs the same hashing work as a wrong password.
func (h *Handler) dummyHash() string {
	h.dummyOnce.Do(func() {
		h.dummy, _ = h.hasher().Hash("requestarr dummy password for timing")
	})
	return h.dummy
}

func (h *Handler) warn(msg string) {
	if h.Logger != nil {
		h.Logger.Warn(msg)
	}
}

// audit writes one row; the error is the caller's to act on.
func (h *Handler) audit(ctx context.Context, typ, userID, username string, r *http.Request) error {
	return h.Local.RecordAuthEvent(ctx, AuditEvent{Type: typ, UserID: userID, Username: username, RemoteAddr: clientAddr(r), At: h.now().UTC()})
}

// clientAddr is the peer address without its port. Forwarding headers are not
// trusted: any client can set them.
func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func normalizeUsername(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if utf8.RuneCountInString(s) > 64 {
		return string([]rune(s)[:64])
	}
	return s
}

// readJSON decodes a small strict JSON body.
func readJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(into) != nil || requireEOF(d) != nil {
		apierror.Write(w, r, http.StatusBadRequest, "invalid_request", "request body is invalid")
		return false
	}
	return true
}

func writeInvalidCredentials(w http.ResponseWriter, r *http.Request) {
	apierror.Write(w, r, http.StatusUnauthorized, "invalid_credentials", "Incorrect username or password.")
}

func writeThrottled(w http.ResponseWriter, r *http.Request, wait time.Duration) {
	secs := int((wait + time.Second - 1) / time.Second)
	w.Header().Set("Retry-After", itoa(secs))
	apierror.Write(w, r, http.StatusTooManyRequests, "too_many_attempts", "Too many attempts. Wait a moment and try again.")
}

func itoa(n int) string {
	if n <= 0 {
		return "1"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// localLogin signs the local administrator in. It is unauthenticated, so it
// carries its own cross-site defence: a matching Origin and a JSON content
// type (a cross-site form cannot send either), on top of SameSite cookies.
func (h *Handler) localLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.Local == nil {
		apierror.Write(w, r, http.StatusServiceUnavailable, "local_sign_in_unavailable", "local sign-in is unavailable")
		return
	}
	if !sameOrigin(r, h.PublicURL) {
		apierror.Write(w, r, http.StatusForbidden, "origin_rejected", "origin rejected")
		return
	}
	if !jsonContentType(r) {
		apierror.Write(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "send application/json")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	account, addr := normalizeUsername(in.Username), clientAddr(r)
	ctx := r.Context()
	if wait := h.throttle().Wait(account, addr); wait > 0 {
		if h.audit(ctx, AuditSignInThrottled, "", "", r) != nil {
			h.warn("could not write the authentication audit")
		}
		writeThrottled(w, r, wait)
		return
	}
	cred, err := h.Local.LocalCredentialByUsername(ctx, account)
	known := err == nil
	if err != nil && !errors.Is(err, ErrNoCredential) {
		apierror.Write(w, r, http.StatusInternalServerError, "auth_unavailable", "auth unavailable")
		return
	}
	stored := h.dummyHash()
	if known {
		stored = cred.PasswordHash
	}
	// One verification whatever was wrong, so timing does not say which.
	matched := h.hasher().Verify(in.Password, stored)
	if !known || !matched || cred.Disabled {
		h.throttle().Fail(account, addr)
		name := ""
		if known {
			name = cred.Username
		}
		if h.audit(ctx, AuditSignInFailure, cred.UserID, name, r) != nil {
			h.warn("could not write the authentication audit")
		}
		writeInvalidCredentials(w, r)
		return
	}
	// A success that cannot be audited is refused.
	if err := h.audit(ctx, AuditSignInSuccess, cred.UserID, cred.Username, r); err != nil {
		apierror.Write(w, r, http.StatusInternalServerError, "auth_unavailable", "auth unavailable")
		return
	}
	h.throttle().Succeed(account, addr)
	if err := h.issueSession(w, r, cred.UserID); err != nil {
		apierror.Write(w, r, http.StatusInternalServerError, "auth_unavailable", "auth unavailable")
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		User               User `json:"user"`
		MustChangePassword bool `json:"mustChangePassword"`
	}{User{ID: cred.UserID, Role: "admin", DisplayName: "Administrator"}, cred.MustChange})
}

func jsonContentType(r *http.Request) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && media == "application/json"
}

// changePassword lets the signed-in local administrator choose a new password.
// It runs behind the session and CSRF checks (see Register).
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _, _, err := h.current(r)
	if err != nil {
		apierror.Write(w, r, http.StatusUnauthorized, "authentication_required", "authentication required")
		return
	}
	if h.Local == nil {
		apierror.Write(w, r, http.StatusServiceUnavailable, "local_sign_in_unavailable", "local sign-in is unavailable")
		return
	}
	ctx := r.Context()
	cred, err := h.Local.LocalCredentialByUserID(ctx, user.ID)
	if errors.Is(err, ErrNoCredential) || (err == nil && cred.Disabled) {
		apierror.Write(w, r, http.StatusForbidden, "forbidden", "this account has no local password")
		return
	}
	if err != nil {
		apierror.Write(w, r, http.StatusInternalServerError, "auth_unavailable", "auth unavailable")
		return
	}
	account, addr := normalizeUsername(cred.Username), clientAddr(r)
	if wait := h.throttle().Wait(account, addr); wait > 0 {
		writeThrottled(w, r, wait)
		return
	}
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	// Always verify the current password first, so a wrong one costs the same
	// whatever else is wrong with the request.
	if !h.hasher().Verify(in.CurrentPassword, cred.PasswordHash) {
		h.throttle().Fail(account, addr)
		if h.audit(ctx, AuditPasswordChangeFailure, cred.UserID, cred.Username, r) != nil {
			h.warn("could not write the authentication audit")
		}
		apierror.Write(w, r, http.StatusForbidden, "current_password_incorrect", "The current password is incorrect.")
		return
	}
	if !password.ValidLength(in.NewPassword) {
		apierror.Write(w, r, http.StatusBadRequest, "invalid_password", "Choose a password of 12 to 128 characters.")
		return
	}
	if in.NewPassword == in.CurrentPassword {
		apierror.Write(w, r, http.StatusBadRequest, "password_unchanged", "Choose a password different from the current one.")
		return
	}
	hash, err := h.hasher().Hash(in.NewPassword)
	if err != nil {
		apierror.Write(w, r, http.StatusInternalServerError, "auth_unavailable", "auth unavailable")
		return
	}
	cookie, _ := r.Cookie(sessionCookie)
	if err := h.Local.ChangeLocalPassword(ctx, cred.UserID, hash, Hash(h.Secret, cookie.Value), h.now().UTC()); err != nil {
		apierror.Write(w, r, http.StatusInternalServerError, "password_change_failed", "could not change the password")
		return
	}
	h.throttle().Succeed(account, addr)
	if h.audit(ctx, AuditPasswordChanged, cred.UserID, cred.Username, r) != nil {
		h.warn("could not write the authentication audit")
	}
	w.WriteHeader(http.StatusNoContent)
}

// allowedWhileMustChange lists the only API paths a must-change session may
// use (paths as routed, without the /api/v1 prefix).
var allowedWhileMustChange = map[string]bool{"/auth/me": true, "/auth/logout": true, "/auth/local/password": true}

// AllowedWhileMustChange reports whether a session that must still change its
// password may call the API path.
func AllowedWhileMustChange(path string) bool { return allowedWhileMustChange[path] }
