package auth

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/netip"
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

// ErrCurrentPasswordIncorrect means the current password did not match the
// stored hash at the moment of the change.
var ErrCurrentPasswordIncorrect = errors.New("current password incorrect")

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
	// Count is how many attempts the row stands for; 0 means 1. Only failure
	// and throttle rows are ever coalesced.
	Count int
}

// LocalStore is the storage the local account needs.
type LocalStore interface {
	LocalCredentialByUsername(ctx context.Context, username string) (LocalCredential, error)
	LocalCredentialByUserID(ctx context.Context, userID string) (LocalCredential, error)
	// CreateLocalSession inserts a session only if the verified credential is still
	// current and enabled, serialized with password changes, and records its
	// successful sign-in audit atomically.
	CreateLocalSession(ctx context.Context, credential LocalCredential, tokenHash, csrfHash []byte, expiry time.Time, remoteAddr string, now time.Time) error
	// ChangeLocalPassword stores the new hash, clears must-change and ends
	// every session of the user except the one with token hash keep, and records
	// exactly one password_changed audit event with remoteAddr, atomically.
	//
	// verify, when not nil, is called with the stored hash while the credential
	// row is locked, in the same transaction as the update; if it returns false
	// nothing changes and ErrCurrentPasswordIncorrect is returned. That makes
	// "current password is right" and "replace it" one step, so two concurrent
	// changes cannot both succeed. Nil skips the check (administrative callers).
	ChangeLocalPassword(ctx context.Context, userID, hash string, keep []byte, remoteAddr string, now time.Time, verify func(storedHash string) bool) error
	RecordAuthEvent(ctx context.Context, e AuditEvent) error
	// RecordAuthEventID is RecordAuthEvent returning the row's id, so later
	// attempts folded into it can be added with AddAuthEventCount.
	RecordAuthEventID(ctx context.Context, e AuditEvent) (int64, error)
	AddAuthEventCount(ctx context.Context, id int64, n int) error
	// PruneAuthEvents deletes audit rows created before the cutoff.
	PruneAuthEvents(ctx context.Context, before time.Time) (int64, error)
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
	return h.Local.RecordAuthEvent(ctx, AuditEvent{Type: typ, UserID: userID, Username: username, RemoteAddr: h.clientAddr(r), At: h.now().UTC()})
}

// clientAddr is the address a request is attributed to, for the sign-in delay
// and the audit. It is the TCP peer address, unless the peer is inside the
// operator's trusted proxy set (D040): then it is the rightmost X-Forwarded-For
// entry that is not itself a trusted proxy. Any client can send that header, so
// it is believed only from a trusted peer and only from the right, where the
// operator's own proxies wrote it. Anything unparseable falls back to the peer.
func (h *Handler) clientAddr(r *http.Request) string {
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	if len(h.TrustedProxies) == 0 {
		return peer
	}
	peerIP, err := netip.ParseAddr(peer)
	if err != nil || !h.trusts(peerIP) {
		return peer
	}
	var hops []string
	for _, line := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(line, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return peer
		}
		if !h.trusts(ip) {
			return ip.Unmap().String()
		}
	}
	return peer
}

func (h *Handler) trusts(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range h.TrustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// hold delays a failed response by d, ending early if the client goes away.
// At most maxHolds responses are held at once; beyond that none is, since the
// verification bounds already limit how fast guesses can be made.
func (h *Handler) hold(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	end, ok := h.throttle().beginHold()
	if !ok {
		return
	}
	defer end()
	if h.Sleep != nil {
		h.Sleep(ctx, d)
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
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
	account, addr := normalizeUsername(in.Username), h.clientAddr(r)
	ctx := r.Context()
	release, ok := h.throttle().Admit(ctx, account, addr)
	if !ok {
		if h.auditAttempt(ctx, AuditSignInThrottled, "", "", account, addr) != nil {
			h.warn("could not write the authentication audit")
		}
		writeThrottled(w, r, time.Second)
		return
	}
	defer release()
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
		delay := h.throttle().Fail(account, addr)
		name := ""
		if known {
			name = cred.Username
		}
		if h.auditAttempt(ctx, AuditSignInFailure, cred.UserID, name, account, addr) != nil {
			h.warn("could not write the authentication audit")
		}
		release()
		h.hold(ctx, delay)
		writeInvalidCredentials(w, r)
		return
	}
	// Session creation checks the verified credential and writes its successful
	// audit together. A credential changed meanwhile is a failed sign-in.
	if err := h.issueSessionUsing(w, r, cred.UserID, func(ctx context.Context, _ string, token, csrf []byte, expiry time.Time) error {
		return h.Local.CreateLocalSession(ctx, cred, token, csrf, expiry, addr, h.now().UTC())
	}); err != nil {
		if errors.Is(err, ErrNoCredential) {
			delay := h.throttle().Fail(account, addr)
			if h.auditAttempt(ctx, AuditSignInFailure, cred.UserID, cred.Username, account, addr) != nil {
				h.warn("could not write the authentication audit")
			}
			release()
			h.hold(ctx, delay)
			writeInvalidCredentials(w, r)
			return
		}
		apierror.Write(w, r, http.StatusInternalServerError, "auth_unavailable", "auth unavailable")
		return
	}
	h.throttle().Succeed(account, addr)
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
	account, addr := normalizeUsername(cred.Username), h.clientAddr(r)
	release, ok := h.throttle().Admit(ctx, account, addr)
	if !ok {
		writeThrottled(w, r, time.Second)
		return
	}
	defer release()
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !readJSON(w, r, &in) {
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
	// The current password is verified inside the store, against the hash read
	// under the credential's row lock, so a concurrent change cannot slip past.
	err = h.Local.ChangeLocalPassword(ctx, cred.UserID, hash, Hash(h.Secret, cookie.Value), addr, h.now().UTC(), func(stored string) bool {
		return h.hasher().Verify(in.CurrentPassword, stored)
	})
	if errors.Is(err, ErrCurrentPasswordIncorrect) {
		delay := h.throttle().Fail(account, addr)
		if h.audit(ctx, AuditPasswordChangeFailure, cred.UserID, cred.Username, r) != nil {
			h.warn("could not write the authentication audit")
		}
		release()
		h.hold(ctx, delay)
		apierror.Write(w, r, http.StatusForbidden, "current_password_incorrect", "The current password is incorrect.")
		return
	}
	if err != nil {
		apierror.Write(w, r, http.StatusInternalServerError, "password_change_failed", "could not change the password")
		return
	}
	h.throttle().Succeed(account, addr)
	w.WriteHeader(http.StatusNoContent)
}

// allowedWhileMustChange lists the only API paths a must-change session may
// use (paths as routed, without the /api/v1 prefix).
var allowedWhileMustChange = map[string]bool{"/auth/me": true, "/auth/logout": true, "/auth/local/password": true}

// AllowedWhileMustChange reports whether a session that must still change its
// password may call the API path.
func AllowedWhileMustChange(path string) bool { return allowedWhileMustChange[path] }
