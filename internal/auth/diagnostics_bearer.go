package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
)

// The bearer side of diagnostics tokens (D043): judging an
// "Authorization: Bearer rqd_..." header. The route table decides which routes
// accept a token at all; this decides whether the token is good. Nothing here
// logs or returns the value, only a fixed reason.
const (
	// DiagnosticsRateLimit is how many requests one token may make per
	// DiagnosticsRateWindow.
	DiagnosticsRateLimit  = 60
	DiagnosticsRateWindow = time.Minute
)

// BearerReason is the fixed vocabulary for logging a refused bearer request.
// The first four come from DiagnosticsRefusalReason.
type BearerReason string

const (
	BearerMalformed   BearerReason = "malformed"
	BearerThrottled   BearerReason = "throttled"
	BearerRateLimited BearerReason = "rate_limited"
	BearerNotReadable BearerReason = "not_readable"
	BearerUnavailable BearerReason = "unavailable"
)

// BearerError is a refused bearer request. Status is 401 for every bad token,
// whatever the reason; 429 (with RetryAfter) when the address is held or the
// token is over its rate; 503 when the token store failed.
type BearerError struct {
	Status     int
	Reason     string
	RetryAfter time.Duration
}

func (e *BearerError) Error() string { return "diagnostics bearer refused" }

// DiagnosticsPrincipal is the person a diagnostics token acts as: a superuser
// identified by the token, so nothing it does is attributed to the creator.
func DiagnosticsPrincipal(tok DiagnosticsToken) User {
	return User{ID: "diagnostics-token:" + tok.ID, Role: string(domain.DiagnosticsTokenRole), DisplayName: "Diagnostics token " + tok.Name}
}

// bearerState is the per-process memory of the bearer gate.
type bearerState struct {
	mu      sync.Mutex
	windows map[string]*rateWindow
	touched map[string]time.Time
}

type rateWindow struct {
	start time.Time
	count int
}

// HasBearerHeader reports whether the request carries any Authorization header.
// Such a request is judged as a token request only.
func HasBearerHeader(r *http.Request) bool { return len(r.Header.Values("Authorization")) > 0 }

// RefuseBearer logs a refusal of a bearer request that never reached a token
// check (a route that does not take tokens) and returns the error to answer.
func (h *Handler) RefuseBearer(r *http.Request, reason BearerReason) *BearerError {
	h.warnBearer(r, string(reason))
	return &BearerError{Status: http.StatusUnauthorized, Reason: string(reason)}
}

// AuthenticateBearer judges the Authorization header of r. On success it is the
// principal the token acts as. Failures count against the client address;
// a held address is refused before the token is looked at.
func (h *Handler) AuthenticateBearer(r *http.Request) (User, *BearerError) {
	ctx := r.Context()
	addr := h.clientAddr(r)
	fail := func(status int, reason string, retry time.Duration) (User, *BearerError) {
		h.warnBearer(r, reason)
		return User{}, &BearerError{Status: status, Reason: reason, RetryAfter: retry}
	}
	if h.Diagnostics == nil {
		return fail(http.StatusUnauthorized, string(DiagnosticsUnknown), 0)
	}
	if wait := h.throttle().AddressHeld(addr); wait > 0 {
		return fail(http.StatusTooManyRequests, string(BearerThrottled), wait)
	}
	refuse := func(reason string) (User, *BearerError) {
		h.throttle().FailAddress(addr)
		return fail(http.StatusUnauthorized, reason, 0)
	}
	value, ok := bearerValue(r)
	if !ok {
		return refuse(string(BearerMalformed))
	}
	tok, err := h.Diagnostics.Lookup(ctx, value)
	if err != nil {
		var refusal *DiagnosticsRefusal
		if errors.As(err, &refusal) {
			return refuse(string(refusal.Reason))
		}
		return fail(http.StatusServiceUnavailable, string(BearerUnavailable), 0)
	}
	now := h.now()
	if wait, over := h.bearer.take(tok.ID, now); over {
		return fail(http.StatusTooManyRequests, string(BearerRateLimited), wait)
	}
	if h.bearer.dueTouch(tok.ID, now) {
		if err := h.Diagnostics.Touch(ctx, tok.ID); err != nil {
			h.log(ctx, slog.LevelWarn, "could not record the use of a diagnostics token", "reason", string(BearerUnavailable))
		}
	}
	return DiagnosticsPrincipal(tok), nil
}

func (h *Handler) warnBearer(r *http.Request, reason string) {
	h.log(r.Context(), slog.LevelWarn, "diagnostics token refused", "reason", reason, "client_addr", h.clientAddr(r), "method", r.Method, "path", r.URL.Path)
}

// bearerValue is the token after exactly one "Bearer " (scheme case-insensitive),
// or false for no header, several headers, another scheme, or stray spaces.
func bearerValue(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	scheme, value, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || value == "" || strings.ContainsAny(value, " \t") {
		return "", false
	}
	return value, true
}

// take counts one request against the token's window. When the token is over
// its rate it returns how long until the window ends.
func (s *bearerState) take(id string, now time.Time) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.windows == nil {
		s.windows = map[string]*rateWindow{}
	}
	w := s.windows[id]
	if w == nil || !now.Before(w.start.Add(DiagnosticsRateWindow)) {
		if len(s.windows) > 64 { // only a few tokens exist; drop the stale ones
			for k, v := range s.windows {
				if !now.Before(v.start.Add(DiagnosticsRateWindow)) {
					delete(s.windows, k)
				}
			}
		}
		w = &rateWindow{start: now}
		s.windows[id] = w
	}
	if w.count >= DiagnosticsRateLimit {
		return w.start.Add(DiagnosticsRateWindow).Sub(now), true
	}
	w.count++
	return 0, false
}

// dueTouch reports whether last_used_at should be written now, and remembers
// that it was, so the store is asked at most once a minute per token.
func (s *bearerState) dueTouch(id string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.touched == nil {
		s.touched = map[string]time.Time{}
	}
	if last, ok := s.touched[id]; ok && now.Sub(last) < DiagnosticsTokenTouchInterval {
		return false
	}
	s.touched[id] = now
	return true
}
