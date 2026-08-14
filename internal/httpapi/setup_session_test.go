package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/buildinfo"
	"git.example.com/evilperson1337/Requestarr/internal/setup"
)

type setupExchangerFunc func(context.Context, string, netip.Addr) setup.BootstrapResult

func (f setupExchangerFunc) Exchange(ctx context.Context, token string, peer netip.Addr) setup.BootstrapResult {
	return f(ctx, token, peer)
}

func TestSetupSessionSuccessCookieContracts(t *testing.T) {
	expires := time.Date(2026, 8, 14, 20, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name       string
		secure     bool
		cookieName string
	}{
		{name: "production", secure: true, cookieName: "__Host-requestarr_setup"},
		{name: "loopback development", cookieName: "requestarr_setup_dev"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			exchanger := setupExchangerFunc(func(_ context.Context, token string, peer netip.Addr) setup.BootstrapResult {
				if token != httpSetupToken() || peer.String() != "192.0.2.10" {
					t.Fatalf("exchange inputs token length=%d peer=%s", len(token), peer)
				}
				return setup.BootstrapResult{Outcome: setup.BootstrapSessionCreated, Session: httpSessionValue(), ExpiresAt: expires}
			})
			response := httptest.NewRecorder()
			request := validSetupSessionRequest()
			handlerWithSetupExchange(exchanger, tt.secure, io.Discard).ServeHTTP(response, request)
			if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
				t.Fatalf("response = %d %q, want 204 empty", response.Code, response.Body.String())
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies = %v, want one", cookies)
			}
			cookie := cookies[0]
			if cookie.Name != tt.cookieName || cookie.Value != httpSessionValue() || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != 900 || cookie.Secure != tt.secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || !cookie.Expires.Equal(expires) {
				t.Fatalf("cookie = %+v", cookie)
			}
			assertSetupTransportHeaders(t, response)
		})
	}
}

func TestSetupSessionOutcomeContracts(t *testing.T) {
	for _, tt := range []struct {
		name        string
		result      setup.BootstrapResult
		wantStatus  int
		wantCode    string
		wantMessage string
		wantRetry   string
	}{
		{name: "invalid token", result: setup.BootstrapResult{Outcome: setup.BootstrapTokenInvalid}, wantStatus: http.StatusUnauthorized, wantCode: "setup_token_invalid", wantMessage: "The setup token is invalid."},
		{name: "locked", result: setup.BootstrapResult{Outcome: setup.BootstrapSetupLocked}, wantStatus: http.StatusLocked, wantCode: "setup_locked", wantMessage: "Setup is locked by server configuration."},
		{name: "completed", result: setup.BootstrapResult{Outcome: setup.BootstrapSetupCompleted}, wantStatus: http.StatusConflict, wantCode: "setup_completed", wantMessage: "Setup has already been completed."},
		{name: "rate rounded", result: setup.BootstrapResult{Outcome: setup.BootstrapRateLimited, RetryAfter: 1500 * time.Millisecond}, wantStatus: http.StatusTooManyRequests, wantCode: "setup_rate_limited", wantMessage: "Too many setup attempts. Try again later.", wantRetry: "2"},
		{name: "rate lower clamp", result: setup.BootstrapResult{Outcome: setup.BootstrapRateLimited}, wantStatus: http.StatusTooManyRequests, wantCode: "setup_rate_limited", wantMessage: "Too many setup attempts. Try again later.", wantRetry: "1"},
		{name: "rate upper clamp", result: setup.BootstrapResult{Outcome: setup.BootstrapRateLimited, RetryAfter: time.Hour}, wantStatus: http.StatusTooManyRequests, wantCode: "setup_rate_limited", wantMessage: "Too many setup attempts. Try again later.", wantRetry: "900"},
		{name: "unavailable", result: setup.BootstrapResult{Outcome: setup.BootstrapUnavailable}, wantStatus: http.StatusServiceUnavailable, wantCode: "setup_unavailable", wantMessage: "Setup is temporarily unavailable."},
		{name: "unknown fail closed", result: setup.BootstrapResult{}, wantStatus: http.StatusServiceUnavailable, wantCode: "setup_unavailable", wantMessage: "Setup is temporarily unavailable."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handlerWithSetupExchange(setupExchangerFunc(func(context.Context, string, netip.Addr) setup.BootstrapResult { return tt.result }), true, io.Discard).ServeHTTP(response, validSetupSessionRequest())
			assertExactSetupError(t, response, tt.wantStatus, tt.wantCode, tt.wantMessage)
			if got := response.Header().Get("Retry-After"); got != tt.wantRetry {
				t.Fatalf("Retry-After = %q, want %q", got, tt.wantRetry)
			}
			if response.Header().Get("Set-Cookie") != "" {
				t.Fatalf("failure set cookie: %s", response.Header().Get("Set-Cookie"))
			}
			assertSetupTransportHeaders(t, response)
		})
	}
}

func TestSetupSessionMethodOriginAndBodyPrecedence(t *testing.T) {
	called := 0
	exchanger := setupExchangerFunc(func(context.Context, string, netip.Addr) setup.BootstrapResult {
		called++
		return setup.BootstrapResult{Outcome: setup.BootstrapTokenInvalid}
	})
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		t.Run("method "+method, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := validSetupSessionRequest()
			request.Method = method
			handlerWithSetupExchange(exchanger, true, io.Discard).ServeHTTP(response, request)
			assertExactSetupError(t, response, http.StatusMethodNotAllowed, "method_not_allowed", "The requested method is not allowed for this resource.")
			if got := response.Header().Values("Allow"); len(got) != 1 || got[0] != http.MethodPost {
				t.Fatalf("Allow = %v, want [POST]", got)
			}
		})
	}

	tests := []struct {
		name       string
		mutate     func(*http.Request)
		wantStatus int
		wantCode   string
	}{
		{name: "missing origin precedes bad body", mutate: func(r *http.Request) { r.Header.Del("Origin"); r.Body = io.NopCloser(strings.NewReader("bad")) }, wantStatus: 403, wantCode: "setup_origin_forbidden"},
		{name: "multiple origin", mutate: func(r *http.Request) {
			r.Header.Add("Origin", "https://requestarr.example")
		}, wantStatus: 403, wantCode: "setup_origin_forbidden"},
		{name: "null origin", mutate: func(r *http.Request) { r.Header.Set("Origin", "null") }, wantStatus: 403, wantCode: "setup_origin_forbidden"},
		{name: "mismatched origin", mutate: func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, wantStatus: 403, wantCode: "setup_origin_forbidden"},
		{name: "origin path", mutate: func(r *http.Request) { r.Header.Set("Origin", "https://requestarr.example/path") }, wantStatus: 403, wantCode: "setup_origin_forbidden"},
		{name: "missing content type", mutate: func(r *http.Request) { r.Header.Del("Content-Type") }, wantStatus: 400, wantCode: "invalid_request"},
		{name: "content type parameter", mutate: func(r *http.Request) { r.Header.Set("Content-Type", "application/json; charset=utf-8") }, wantStatus: 400, wantCode: "invalid_request"},
		{name: "multiple content type", mutate: func(r *http.Request) { r.Header.Add("Content-Type", "application/json") }, wantStatus: 400, wantCode: "invalid_request"},
		{name: "empty body", mutate: func(r *http.Request) { r.Body = http.NoBody }, wantStatus: 400, wantCode: "invalid_request"},
		{name: "malformed JSON", mutate: func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(`{"token":`)) }, wantStatus: 400, wantCode: "invalid_request"},
		{name: "unknown field", mutate: func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(`{"token":"` + httpSetupToken() + `","extra":1}`))
		}, wantStatus: 400, wantCode: "invalid_request"},
		{name: "duplicate token field", mutate: func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(`{"token":"` + httpSetupToken() + `","token":"` + httpSetupToken() + `"}`))
		}, wantStatus: 400, wantCode: "invalid_request"},
		{name: "multiple JSON", mutate: func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(`{"token":"` + httpSetupToken() + `"}{}`))
		}, wantStatus: 400, wantCode: "invalid_request"},
		{name: "missing token", mutate: func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(`{}`)) }, wantStatus: 400, wantCode: "invalid_request"},
		{name: "non-string token", mutate: func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(`{"token":1}`)) }, wantStatus: 400, wantCode: "invalid_request"},
		{name: "malformed token encoding", mutate: func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(`{"token":"short"}`)) }, wantStatus: 400, wantCode: "invalid_request"},
		{name: "oversized", mutate: func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 129))) }, wantStatus: 400, wantCode: "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := called
			request := validSetupSessionRequest()
			tt.mutate(request)
			response := httptest.NewRecorder()
			handlerWithSetupExchange(exchanger, true, io.Discard).ServeHTTP(response, request)
			assertAPIError(t, response, tt.wantStatus, tt.wantCode)
			if called != before {
				t.Fatal("rejected transport input reached setup service")
			}
			if response.Header().Get("Set-Cookie") != "" {
				t.Fatal("rejected request set cookie")
			}
		})
	}
}

func TestSetupSessionExchangeDoesNotRequireMutationCSRFHeader(t *testing.T) {
	request := validSetupSessionRequest()
	request.Header.Del("X-Requestarr-CSRF")
	response := httptest.NewRecorder()
	handlerWithSetupExchange(setupExchangerFunc(func(context.Context, string, netip.Addr) setup.BootstrapResult {
		return setup.BootstrapResult{Outcome: setup.BootstrapTokenInvalid}
	}), true, io.Discard).ServeHTTP(response, request)
	assertExactSetupError(t, response, http.StatusUnauthorized, "setup_token_invalid", "The setup token is invalid.")
}

func TestSetupSessionUsesDirectPeerIgnoresForwardedHeadersAndBoundsContext(t *testing.T) {
	exchanger := setupExchangerFunc(func(ctx context.Context, _ string, peer netip.Addr) setup.BootstrapResult {
		if peer.String() != "192.0.2.10" {
			t.Fatalf("peer = %s, want direct RemoteAddr", peer)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > setupSessionTimeout {
			t.Fatalf("deadline = %v, ok=%t", deadline, ok)
		}
		return setup.BootstrapResult{Outcome: setup.BootstrapTokenInvalid}
	})
	request := validSetupSessionRequest()
	request.Header.Set("Forwarded", "for=203.0.113.1;proto=http;host=evil.example")
	request.Header.Set("X-Forwarded-For", "203.0.113.2")
	request.Header.Set("X-Real-IP", "203.0.113.3")
	response := httptest.NewRecorder()
	handlerWithSetupExchange(exchanger, true, io.Discard).ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusUnauthorized, "setup_token_invalid")
}

func TestSetupSessionUnavailableForInvalidDirectPeerOrMissingService(t *testing.T) {
	for _, tt := range []struct {
		name      string
		exchanger SetupSessionExchanger
		remote    string
	}{
		{name: "missing service", remote: "192.0.2.10:1234"},
		{name: "invalid peer", exchanger: setupExchangerFunc(func(context.Context, string, netip.Addr) setup.BootstrapResult {
			t.Fatal("service called")
			return setup.BootstrapResult{}
		}), remote: "proxy.example:1234"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := validSetupSessionRequest()
			request.RemoteAddr = tt.remote
			response := httptest.NewRecorder()
			handlerWithSetupExchange(tt.exchanger, true, io.Discard).ServeHTTP(response, request)
			assertExactSetupError(t, response, 503, "setup_unavailable", "Setup is temporarily unavailable.")
		})
	}
}

func TestSetupSessionLogsAndResponsesRedactSensitiveInputs(t *testing.T) {
	var logs bytes.Buffer
	originSecret := "https://origin-secret.example"
	bodySecret := httpSetupToken()
	request := validSetupSessionRequest()
	request.Header.Set("Origin", originSecret)
	request.Header.Set("Authorization", "Bearer authorization-secret")
	request.Header.Set("Cookie", "secret-cookie=value")
	request.Header.Set("X-Requestarr-CSRF", "csrf-secret")
	request.URL.RawQuery = "token=query-secret"
	request.Header.Set(requestIDHeader, "setup-log-id")
	response := httptest.NewRecorder()
	handlerWithSetupExchange(nil, true, &logs).ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusForbidden, "setup_origin_forbidden")
	combined := logs.String() + response.Body.String()
	for _, secret := range []string{originSecret, bodySecret, "authorization-secret", "secret-cookie", "csrf-secret", "query-secret", "token="} {
		if strings.Contains(combined, secret) {
			t.Fatalf("logs/response exposed %q: %s", secret, combined)
		}
	}
	for _, want := range []string{`"request_id":"setup-log-id"`, `"route":"/api/v1/setup/session"`, `"status":403`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log lacks %q: %s", want, logs.String())
		}
	}
}

func validSetupSessionRequest() *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/session", strings.NewReader(`{"token":"`+httpSetupToken()+`"}`))
	request.RemoteAddr = "192.0.2.10:4567"
	request.Header.Set("Origin", "https://requestarr.example")
	request.Header.Set("Content-Type", "application/json")
	return request
}

func handlerWithSetupExchange(exchanger SetupSessionExchanger, secure bool, output io.Writer) http.Handler {
	logger := slog.New(slog.NewJSONHandler(output, nil))
	return New(logger, buildinfo.Info{}, nil, successfulSetupStatus(false), SetupSessionOptions{
		Exchanger: exchanger, PublicOrigin: "https://requestarr.example:443", SecureCookie: secure,
	})
}

func httpSetupToken() string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 32))
}

func httpSessionValue() string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x52}, 32))
}

func assertExactSetupError(t *testing.T, response *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	assertAPIError(t, response, status, code)
	var envelope apiErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Message != message {
		t.Fatalf("message = %q, want %q", envelope.Error.Message, message)
	}
}

func assertSetupTransportHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	for name, want := range map[string]string{
		"Cache-Control":          "no-store",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	} {
		if got := response.Header().Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if response.Header().Get(requestIDHeader) == "" {
		t.Fatal("missing request ID")
	}
}
