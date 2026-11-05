package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Pause at the session boundary after the handler verified the credential,
// before it attempts session insertion, without holding the credential lock.
type credentialBarrierStore struct {
	*fakeLocalStore
	read   chan struct{}
	resume chan struct{}
}

func (s *credentialBarrierStore) CreateLocalSession(ctx context.Context, cred LocalCredential, token, csrf []byte, expiry time.Time, remoteAddr string, now time.Time) error {
	close(s.read)
	select {
	case <-s.resume:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.fakeLocalStore.CreateLocalSession(ctx, cred, token, csrf, expiry, remoteAddr, now)
}

func awaitAttempt[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-ctx.Done():
		t.Fatal("password attempt did not reach its synchronization boundary")
		var zero T
		return zero
	}
}

func TestSignInRejectsCredentialChangedAfterRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	f := newFixture(t)
	barrier := &credentialBarrierStore{fakeLocalStore: f.store, read: make(chan struct{}), resume: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(barrier.resume) }) }
	defer release()
	f.h.Local = barrier
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.login("admin", testPassword, func(r *http.Request) { *r = *r.WithContext(ctx) }) }()
	awaitAttempt(t, ctx, barrier.read)
	hash, err := f.hasher.Hash(goodNewPassword)
	if err != nil {
		t.Fatal("could not prepare replacement password")
	}
	if err := f.store.ChangeLocalPassword(context.Background(), adminUserID, hash, nil, "203.0.113.7", f.clock.Now()); err != nil {
		t.Fatal("password change failed")
	}
	release()
	w := awaitAttempt(t, ctx, done)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("stale sign-in status=%d, want 401", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Error("stale sign-in issued cookies")
	}
	if n := f.store.liveSessions(adminUserID); n != 0 {
		t.Errorf("live sessions=%d, want 0", n)
	}
	var successes, failures int
	for _, event := range f.store.eventTypes() {
		if event == AuditSignInSuccess {
			successes++
		}
		if event == AuditSignInFailure {
			failures++
		}
	}
	if successes != 0 || failures != 1 {
		t.Errorf("rejected stale sign-in audit: successes=%d failures=%d, want 0 and 1", successes, failures)
	}
}

// Only the first verification pauses; an unprotected parallel guess completes,
// making the regression deterministic rather than depending on scheduling.
type verificationBarrier struct {
	PasswordHasher
	ctx     context.Context
	calls   atomic.Int32
	entered chan struct{}
	resume  chan struct{}
}

func (h *verificationBarrier) Verify(pw, encoded string) bool {
	if h.calls.Add(1) == 1 {
		close(h.entered)
		select {
		case <-h.resume:
		case <-h.ctx.Done():
			return false
		}
	}
	return h.PasswordHasher.Verify(pw, encoded)
}

func TestConcurrentPasswordGuessesReserveAccountAndAddress(t *testing.T) {
	for _, tc := range []struct {
		name, user, addr string
		change           bool
	}{
		{name: "same account", user: "admin", addr: "198.51.100.22"},
		{name: "same address", user: "unknown", addr: "203.0.113.7"},
		{name: "password change", change: true, addr: "198.51.100.22"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			f := newFixture(t)
			var cookies []*http.Cookie
			if tc.change {
				cookies = f.signIn()
			}
			barrier := &verificationBarrier{PasswordHasher: f.hasher, ctx: ctx, entered: make(chan struct{}), resume: make(chan struct{})}
			var once sync.Once
			release := func() { once.Do(func() { close(barrier.resume) }) }
			defer release()
			f.h.Hasher = barrier
			attempt := func(user, addr string) *httptest.ResponseRecorder {
				if tc.change {
					return f.authed(http.MethodPost, "/auth/local/password", changeBody("incorrect current passphrase", goodNewPassword), cookies, fromAddr(addr))
				}
				return f.login(user, "incorrect test passphrase", fromAddr(addr))
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- attempt("admin", "203.0.113.7") }()
			awaitAttempt(t, ctx, barrier.entered)
			second := attempt(tc.user, tc.addr)
			calls := barrier.calls.Load()
			release()
			first := awaitAttempt(t, ctx, done)
			if second.Code != http.StatusTooManyRequests {
				t.Errorf("parallel guess status=%d, want 429", second.Code)
			}
			if calls != 1 {
				t.Errorf("parallel verifications=%d, want 1", calls)
			}
			if second.Code == http.StatusTooManyRequests {
				retryAfter(t, second)
			}
			if first.Code != http.StatusUnauthorized && first.Code != http.StatusForbidden {
				t.Errorf("first guess status=%d", first.Code)
			}
			// The reservation is released even on failure; there is no lockout.
			if w := attempt("admin", "203.0.113.7"); w.Code == http.StatusTooManyRequests {
				t.Error("completed attempt retained its reservation")
			}
		})
	}
}
