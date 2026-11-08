package auth

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func bearerHandler(t *testing.T) (*Handler, *fakeClock, *bytes.Buffer, string) {
	t.Helper()
	store := newFakeDiagTokenStore()
	svc, clock := newDiagService(store)
	logs := &bytes.Buffer{}
	h := &Handler{Secret: testSecret, Clock: clock.Now, Diagnostics: svc, Logger: slog.New(slog.NewTextHandler(logs, nil))}
	value, _, err := svc.Create(context.Background(), superuser, "agent", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return h, clock, logs, value
}

func bearerReq(header, addr string) *http.Request {
	r := httptest.NewRequest("GET", "/api/v1/admin/logs", nil)
	r.RemoteAddr = addr + ":4000"
	if header != "" {
		r.Header.Add("Authorization", header)
	}
	return r
}

func TestOnlyOneBearerRqdHeaderIsATokenRequest(t *testing.T) {
	cases := []struct {
		name    string
		headers []string
		want    bool
	}{
		{"none", nil, false},
		{"rqd bearer", []string{"Bearer rqd_abc"}, true},
		{"scheme case-insensitive", []string{"bearer rqd_abc"}, true},
		{"basic from a proxy", []string{"Basic dXNlcjpwdw=="}, false},
		{"another bearer value", []string{"Bearer eyJhbGciOi"}, false},
		{"bare bearer", []string{"Bearer"}, false},
		{"wrong case prefix", []string{"Bearer RQD_abc"}, false},
		{"two headers", []string{"Bearer rqd_abc", "Basic x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			for _, v := range tc.headers {
				r.Header.Add("Authorization", v)
			}
			if got := IsTokenRequest(r); got != tc.want {
				t.Fatalf("IsTokenRequest = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBearerFailuresDoNotHoldSignInAndFailedSignInsDoNotHoldTheToken(t *testing.T) {
	h, _, _, value := bearerHandler(t)
	addr := "203.0.113.5"
	for i := 0; i < 10; i++ {
		if _, err := h.AuthenticateBearer(bearerReq("Bearer rqd_bad", addr)); err == nil {
			t.Fatal("bad token accepted")
		}
	}
	if _, err := h.AuthenticateBearer(bearerReq("Bearer "+value, addr)); err == nil || err.Status != http.StatusTooManyRequests {
		t.Fatalf("the noisy address should be held for tokens: %+v", err)
	}
	if wait := h.throttle().AddressHeld(addr); wait != 0 {
		t.Fatalf("bearer failures held sign-in for %v", wait)
	}

	h2, _, _, value2 := bearerHandler(t)
	for i := 0; i < 10; i++ {
		h2.throttle().Fail("someone", addr)
	}
	if h2.throttle().AddressHeld(addr) == 0 {
		t.Fatal("test setup: sign-in should be held")
	}
	if _, err := h2.AuthenticateBearer(bearerReq("Bearer "+value2, addr)); err != nil {
		t.Fatalf("failed sign-ins held the token: %+v", err)
	}
}

func TestGarbageRqdTokensOnAnUnreadableRouteCountAsFailuresButAValidOneDoesNot(t *testing.T) {
	h, _, _, value := bearerHandler(t)
	addr := "203.0.113.6"
	for i := 0; i < 20; i++ {
		if err := h.RefuseUnreadable(bearerReq("Bearer "+value, addr)); err == nil || err.Status != http.StatusUnauthorized || err.Reason != string(BearerNotReadable) {
			t.Fatalf("valid token on an unmarked route: %+v", err)
		}
	}
	if _, err := h.AuthenticateBearer(bearerReq("Bearer "+value, addr)); err != nil {
		t.Fatalf("a valid token on unmarked routes was counted: %+v", err)
	}
	for _, bad := range []string{"Bearer rqd_", "Bearer rqd_unknown" + strings.Repeat("A", 30)} {
		for i := 0; i < 6; i++ {
			if err := h.RefuseUnreadable(bearerReq(bad, addr)); err == nil {
				t.Fatal("garbage accepted")
			}
		}
	}
	if _, err := h.AuthenticateBearer(bearerReq("Bearer "+value, addr)); err == nil || err.Status != http.StatusTooManyRequests {
		t.Fatalf("garbage attempts on an unmarked route did not throttle: %+v", err)
	}
}

func TestThrottledAndRateLimitedWarningsLogOncePerMinuteWithASuppressedCount(t *testing.T) {
	h, clock, logs, value := bearerHandler(t)
	addr := "203.0.113.7"
	for i := 0; i < 6; i++ {
		_, _ = h.AuthenticateBearer(bearerReq("Bearer rqd_bad", addr))
	}
	logs.Reset()
	for i := 0; i < 50; i++ {
		_, _ = h.AuthenticateBearer(bearerReq("Bearer "+value, addr))
	}
	if n := strings.Count(logs.String(), "reason=throttled"); n != 1 {
		t.Fatalf("throttled warnings in a minute = %d\n%s", n, logs)
	}
	clock.Advance(61 * time.Second)
	_, _ = h.AuthenticateBearer(bearerReq("Bearer rqd_bad", addr)) // held again afterwards
	logs.Reset()
	_, _ = h.AuthenticateBearer(bearerReq("Bearer "+value, addr))
	if !strings.Contains(logs.String(), "reason=throttled") || !strings.Contains(logs.String(), "suppressed=49") {
		t.Fatalf("want one throttled warning counting 49 suppressed:\n%s", logs)
	}

	h2, clock2, logs2, value2 := bearerHandler(t)
	const peer = "198.51.100.1"
	for i := 0; i < DiagnosticsRateLimit; i++ {
		if _, err := h2.AuthenticateBearer(bearerReq("Bearer "+value2, peer)); err != nil {
			t.Fatal(err)
		}
	}
	logs2.Reset()
	for i := 0; i < 30; i++ {
		_, _ = h2.AuthenticateBearer(bearerReq("Bearer "+value2, peer))
	}
	if n := strings.Count(logs2.String(), "reason=rate_limited"); n != 1 {
		t.Fatalf("rate_limited warnings = %d\n%s", n, logs2)
	}
	clock2.Advance(61 * time.Second)
	for i := 0; i < DiagnosticsRateLimit; i++ {
		_, _ = h2.AuthenticateBearer(bearerReq("Bearer "+value2, peer))
	}
	logs2.Reset()
	_, _ = h2.AuthenticateBearer(bearerReq("Bearer "+value2, peer))
	if !strings.Contains(logs2.String(), "suppressed=29") {
		t.Fatalf("want suppressed=29:\n%s", logs2)
	}
}
