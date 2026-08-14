package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/buildinfo"
)

type checkerFunc func(context.Context) error

func (f checkerFunc) Ready(ctx context.Context) error { return f(ctx) }

type setupStatusFunc func(context.Context) (bool, error)

func (f setupStatusFunc) Complete(ctx context.Context) (bool, error) { return f(ctx) }

func TestHealthzIsDependencyFree(t *testing.T) {
	called := false
	handler := testHandler(checkerFunc(func(context.Context) error {
		called = true
		return errors.New("must not be called")
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assertJSONResponse(t, response, http.StatusOK, `{"status":"ok"}`)
	if called {
		t.Fatal("healthz called readiness dependency")
	}
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name       string
		checker    ReadinessChecker
		wantStatus int
		wantBody   string
	}{
		{name: "ready", checker: checkerFunc(func(context.Context) error { return nil }), wantStatus: http.StatusOK, wantBody: `{"status":"ready"}`},
		{name: "unavailable", checker: checkerFunc(func(context.Context) error { return errors.New("postgres://user:password@host/database") }), wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"not_ready","reason":"database_unavailable"}`},
		{name: "missing checker", wantStatus: http.StatusServiceUnavailable, wantBody: `{"status":"not_ready","reason":"database_unavailable"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandler(tt.checker).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			assertJSONResponse(t, response, tt.wantStatus, tt.wantBody)
			if strings.Contains(response.Body.String(), "password") {
				t.Fatal("readiness response exposed dependency error")
			}
		})
	}
}

func TestReadyzBoundsChecker(t *testing.T) {
	handler := testHandler(checkerFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}))
	response := httptest.NewRecorder()
	started := time.Now()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if elapsed := time.Since(started); elapsed < readinessTimeout || elapsed > readinessTimeout+time.Second {
		t.Fatalf("readiness check took %v, expected approximately %v", elapsed, readinessTimeout)
	}
	assertJSONResponse(t, response, http.StatusServiceUnavailable, `{"status":"not_ready","reason":"database_unavailable"}`)
}

func TestAPIUnknownRouteReturnsJSONError(t *testing.T) {
	for _, path := range []string{"/api/v1", "/api/v1/", "/api/v1/missing"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))

			assertAPIError(t, response, http.StatusNotFound, "not_found")
		})
	}
}

func TestAPINamespaceDoesNotCaptureSimilarPrefixes(t *testing.T) {
	for _, path := range []string{"/api", "/api/", "/api/v", "/api/v10", "/api/v10/missing", "/api/v1x", "/api/v1x/missing"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))

			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
			}
			if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Fatalf("Content-Type = %q, want non-API text 404", got)
			}
			if strings.Contains(response.Body.String(), `"request_id"`) {
				t.Fatalf("similar prefix received API envelope: %s", response.Body.String())
			}
		})
	}
}

func TestAPIRouteRejectsUnsupportedMethod(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandler(nil).ServeHTTP(response, httptest.NewRequest(method, "/api/v1/version", nil))

			assertAPIError(t, response, http.StatusMethodNotAllowed, "method_not_allowed")
			if got := response.Header().Values("Allow"); len(got) != 2 || got[0] != http.MethodGet || got[1] != http.MethodHead {
				t.Fatalf("Allow = %v, want [GET HEAD]", got)
			}
		})
	}
}

func TestAPIGetResourcesSupportHEAD(t *testing.T) {
	for _, path := range []string{"/api/v1/version", "/api/v1/setup/status"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodHead, path, nil))

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
		})
	}
}

func TestHEADResponsesHaveNoWireBody(t *testing.T) {
	server := httptest.NewServer(testHandler(nil))
	t.Cleanup(server.Close)

	for _, path := range []string{"/healthz", "/readyz", "/api/v1/version", "/api/v1/setup/status"} {
		t.Run(path, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodHead, server.URL+path, nil)
			if err != nil {
				t.Fatalf("construct HEAD request: %v", err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("perform HEAD request: %v", err)
			}
			body, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if readErr != nil {
				t.Fatalf("read HEAD response: %v", readErr)
			}
			if closeErr != nil {
				t.Fatalf("close HEAD response: %v", closeErr)
			}
			if len(body) != 0 {
				t.Fatalf("HEAD response body = %q, want empty", body)
			}
		})
	}
}

func TestRequestID(t *testing.T) {
	t.Run("accepts valid inbound value", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil)
		request.Header.Set(requestIDHeader, "client-request_123.abc")
		response := httptest.NewRecorder()
		testHandler(nil).ServeHTTP(response, request)

		if got := response.Header().Get(requestIDHeader); got != "client-request_123.abc" {
			t.Fatalf("request ID = %q, want inbound value", got)
		}
		assertErrorRequestIDMatchesHeader(t, response)
	})

	for _, tt := range []struct {
		name string
		id   string
	}{
		{name: "missing"},
		{name: "contains whitespace", id: "secret token"},
		{name: "contains control character", id: "secret\nvalue"},
		{name: "oversized", id: strings.Repeat("a", maxRequestIDLength+1)},
	} {
		t.Run("replaces "+tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil)
			request.Header.Set(requestIDHeader, tt.id)
			response := httptest.NewRecorder()
			testHandler(nil).ServeHTTP(response, request)

			got := response.Header().Get(requestIDHeader)
			if !validRequestID(got) || got == tt.id {
				t.Fatalf("generated request ID = %q, inbound = %q", got, tt.id)
			}
			assertErrorRequestIDMatchesHeader(t, response)
		})
	}

	t.Run("replaces multiple values", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil)
		request.Header[requestIDHeader] = []string{"first", "second"}
		response := httptest.NewRecorder()
		testHandler(nil).ServeHTTP(response, request)

		got := response.Header().Get(requestIDHeader)
		if !validRequestID(got) || got == "first" || got == "second" {
			t.Fatalf("generated request ID = %q", got)
		}
		assertErrorRequestIDMatchesHeader(t, response)
	})

	t.Run("replaces comma-joined values", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil)
		request.Header.Set(requestIDHeader, "first,second")
		response := httptest.NewRecorder()
		testHandler(nil).ServeHTTP(response, request)

		got := response.Header().Get(requestIDHeader)
		if !validRequestID(got) || got == "first,second" {
			t.Fatalf("generated request ID = %q", got)
		}
		assertErrorRequestIDMatchesHeader(t, response)
	})
}

func TestRequestIDPropagatesOnEveryResponseClass(t *testing.T) {
	for _, path := range []string{"/", "/missing", "/healthz", "/readyz", "/api/v1/version", "/api/v1/setup/status", "/api/v1/missing"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set(requestIDHeader, "propagated-request-id")
			response := httptest.NewRecorder()
			testHandler(nil).ServeHTTP(response, request)

			if got := response.Header().Get(requestIDHeader); got != "propagated-request-id" {
				t.Fatalf("request ID = %q, want propagated-request-id", got)
			}
		})
	}
}

func TestAccessLogCapturesStatusWithoutSecrets(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := New(logger, buildinfo.Info{}, nil, successfulSetupStatus(false))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/version?token=query-secret", strings.NewReader("body-secret"))
	request.Header.Set("Authorization", "Bearer header-secret")
	request.Header.Set("Cookie", "session=cookie-secret")
	request.Header.Set(requestIDHeader, "log-request-id")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	line := logs.String()
	for _, want := range []string{`"request_id":"log-request-id"`, `"method":"POST"`, `"route":"/api/v1/version"`, `"status":405`, `"duration":`} {
		if !strings.Contains(line, want) {
			t.Errorf("log %q does not contain %q", line, want)
		}
	}
	for _, secret := range []string{"query-secret", "header-secret", "cookie-secret", "body-secret", "Authorization", "Cookie", "token="} {
		if strings.Contains(line, secret) {
			t.Errorf("log contains secret-bearing value %q: %s", secret, line)
		}
	}
}

func TestAccessLogUsesRoutePatternInsteadOfCallerPath(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := New(logger, buildinfo.Info{}, nil, successfulSetupStatus(false))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/missing/path-secret?token=query-secret", nil)

	handler.ServeHTTP(httptest.NewRecorder(), request)

	line := logs.String()
	if !strings.Contains(line, `"route":"/api/v1/"`) {
		t.Fatalf("log %q does not contain the bounded API route pattern", line)
	}
	for _, secret := range []string{"path-secret", "query-secret", "token="} {
		if strings.Contains(line, secret) {
			t.Errorf("log contains caller-controlled secret-bearing value %q: %s", secret, line)
		}
	}
}

func TestStatusWriterRecordsFinalResponseStatus(t *testing.T) {
	tests := []struct {
		name       string
		write      func(http.ResponseWriter)
		wantStatus int
		wantBody   string
	}{
		{name: "implicit OK without write", write: func(http.ResponseWriter) {}, wantStatus: http.StatusOK},
		{name: "body implies OK", write: func(w http.ResponseWriter) { _, _ = w.Write([]byte("body")) }, wantStatus: http.StatusOK, wantBody: "body"},
		{name: "explicit status", write: func(w http.ResponseWriter) { w.WriteHeader(http.StatusCreated) }, wantStatus: http.StatusCreated},
		{name: "duplicate final status ignored", write: func(w http.ResponseWriter) { w.WriteHeader(http.StatusCreated); w.WriteHeader(http.StatusTeapot) }, wantStatus: http.StatusCreated},
		{name: "informational then final", write: func(w http.ResponseWriter) { w.WriteHeader(http.StatusEarlyHints); w.WriteHeader(http.StatusNoContent) }, wantStatus: http.StatusNoContent},
		{name: "switching protocols is final", write: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusSwitchingProtocols)
			w.WriteHeader(http.StatusOK)
		}, wantStatus: http.StatusSwitchingProtocols},
		{name: "informational after final ignored", write: func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent); w.WriteHeader(http.StatusEarlyHints) }, wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			underlying := httptest.NewRecorder()
			writer := &statusWriter{ResponseWriter: underlying}
			tt.write(writer)
			gotStatus := writer.status
			if gotStatus == 0 {
				gotStatus = http.StatusOK
			}
			if gotStatus != tt.wantStatus {
				t.Fatalf("recorded status = %d, want %d", gotStatus, tt.wantStatus)
			}
			if got := underlying.Body.String(); got != tt.wantBody {
				t.Fatalf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

func TestStatusWriterSupportsResponseController(t *testing.T) {
	underlying := httptest.NewRecorder()
	writer := &statusWriter{ResponseWriter: underlying}

	if err := http.NewResponseController(writer).Flush(); err != nil {
		t.Fatalf("flush through wrapped writer: %v", err)
	}
	if writer.status != http.StatusOK {
		t.Fatalf("recorded status = %d, want %d", writer.status, http.StatusOK)
	}
	if !underlying.Flushed {
		t.Fatal("underlying response was not flushed")
	}
}

func TestSecurityHeadersRemainOnAllPaths(t *testing.T) {
	for _, path := range []string{"/", "/missing", "/healthz", "/api/v1/setup/status", "/api/v1/missing"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			for name, want := range map[string]string{
				"X-Content-Type-Options": "nosniff",
				"X-Frame-Options":        "DENY",
				"Referrer-Policy":        "same-origin",
			} {
				if got := response.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestSetupStatusReturnsExactCompletionOnlyResponse(t *testing.T) {
	tests := []struct {
		name     string
		complete bool
		body     string
	}{
		{name: "incomplete", body: `{"complete":false}`},
		{name: "complete", complete: true, body: `{"complete":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandlerWithSetup(nil, successfulSetupStatus(tt.complete)).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
			assertJSONResponse(t, response, http.StatusOK, tt.body)
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestVersionScaffoldResponseRemainsStable(t *testing.T) {
	response := httptest.NewRecorder()
	testHandler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))
	assertJSONResponse(t, response, http.StatusOK, `{"version":"","commit":"","date":""}`)
}

func TestSetupStatusUnavailableIsSafe(t *testing.T) {
	secret := "postgres://user:password@database/requestarr"
	tests := []struct {
		name   string
		reader SetupStatusReader
	}{
		{name: "missing reader"},
		{name: "invalid persisted state", reader: setupStatusFunc(func(context.Context) (bool, error) { return false, errors.New("malformed step: " + secret) })},
		{name: "database error", reader: setupStatusFunc(func(context.Context) (bool, error) { return false, errors.New(secret) })},
		{name: "canceled", reader: setupStatusFunc(func(context.Context) (bool, error) { return false, context.Canceled })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandlerWithSetup(nil, tt.reader).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
			assertAPIError(t, response, http.StatusServiceUnavailable, "setup_status_unavailable")
			if strings.Contains(response.Body.String(), secret) || strings.Contains(response.Body.String(), "malformed") {
				t.Fatalf("response exposed internal error: %s", response.Body.String())
			}
			var envelope apiErrorEnvelope
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if envelope.Error.Message != "Setup status is temporarily unavailable." {
				t.Fatalf("message = %q", envelope.Error.Message)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestSetupStatusFailureRetainsTransportMiddleware(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := New(logger, buildinfo.Info{}, nil, setupStatusFunc(func(context.Context) (bool, error) {
		return false, errors.New("postgres://user:password@database/requestarr")
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/status?token=query-secret", nil)
	request.Header.Set(requestIDHeader, "setup-request-id")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusServiceUnavailable, "setup_status_unavailable")
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	for name, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	} {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	line := logs.String()
	for _, want := range []string{`"request_id":"setup-request-id"`, `"route":"/api/v1/setup/status"`, `"status":503`} {
		if !strings.Contains(line, want) {
			t.Errorf("log %q does not contain %q", line, want)
		}
	}
	for _, secret := range []string{"password", "query-secret", "token="} {
		if strings.Contains(line, secret) {
			t.Errorf("log contains secret-bearing value %q: %s", secret, line)
		}
	}
}

func TestSetupStatusUnavailableHEADHasNoWireBody(t *testing.T) {
	server := httptest.NewServer(testHandlerWithSetup(nil, setupStatusFunc(func(context.Context) (bool, error) {
		return false, errors.New("unavailable")
	})))
	t.Cleanup(server.Close)
	request, err := http.NewRequest(http.MethodHead, server.URL+"/api/v1/setup/status", nil)
	if err != nil {
		t.Fatalf("construct HEAD request: %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("perform HEAD request: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusServiceUnavailable || len(body) != 0 {
		t.Fatalf("HEAD unavailable = status %d body %q read error %v", response.StatusCode, body, readErr)
	}
}

func TestSetupStatusReadHasBoundedDeadline(t *testing.T) {
	reader := setupStatusFunc(func(ctx context.Context) (bool, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("setup status context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > setupStatusTimeout {
			t.Fatalf("setup status deadline remaining = %v, want within (0, %v]", remaining, setupStatusTimeout)
		}
		return false, context.DeadlineExceeded
	})
	response := httptest.NewRecorder()
	testHandlerWithSetup(nil, reader).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	assertAPIError(t, response, http.StatusServiceUnavailable, "setup_status_unavailable")
}

func TestSetupStatusRejectsUnsupportedMethodWithoutReading(t *testing.T) {
	called := false
	reader := setupStatusFunc(func(context.Context) (bool, error) {
		called = true
		return false, nil
	})
	response := httptest.NewRecorder()
	testHandlerWithSetup(nil, reader).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/setup/status", nil))
	assertAPIError(t, response, http.StatusMethodNotAllowed, "method_not_allowed")
	if called {
		t.Fatal("unsupported method read setup state")
	}
	if got := response.Header().Values("Allow"); len(got) != 2 || got[0] != http.MethodGet || got[1] != http.MethodHead {
		t.Fatalf("Allow = %v, want [GET HEAD]", got)
	}
}

func TestNonAPIAndProbeBehaviorRemainStable(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		wantStatus  int
		wantType    string
		wantBody    string
		bodyContain string
	}{
		{name: "root", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantType: "text/html; charset=utf-8", bodyContain: "Requestarr scaffold"},
		{name: "non-API missing", method: http.MethodGet, path: "/missing", wantStatus: http.StatusNotFound, wantType: "text/plain; charset=utf-8", wantBody: "404 page not found\n"},
		{name: "health HEAD", method: http.MethodHead, path: "/healthz", wantStatus: http.StatusOK, wantType: "application/json"},
		{name: "health unsupported method", method: http.MethodPost, path: "/healthz", wantStatus: http.StatusNotFound, wantType: "text/plain; charset=utf-8", wantBody: "404 page not found\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			testHandler(nil).ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if got := response.Header().Get("Content-Type"); got != tt.wantType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.wantType)
			}
			if got := response.Body.String(); tt.wantBody != "" && got != tt.wantBody {
				t.Fatalf("body = %q, want %q", got, tt.wantBody)
			}
			if !strings.Contains(response.Body.String(), tt.bodyContain) {
				t.Fatalf("body = %q, want it to contain %q", response.Body.String(), tt.bodyContain)
			}
		})
	}
}

func testHandler(checker ReadinessChecker) http.Handler {
	return testHandlerWithSetup(checker, successfulSetupStatus(false))
}

func testHandlerWithSetup(checker ReadinessChecker, setupStatus SetupStatusReader) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(logger, buildinfo.Info{}, checker, setupStatus)
}

func successfulSetupStatus(complete bool) SetupStatusReader {
	return setupStatusFunc(func(context.Context) (bool, error) { return complete, nil })
}

func assertJSONResponse(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, wantBody string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("status = %d, want %d", response.Code, wantStatus)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := strings.TrimSpace(response.Body.String()); got != wantBody {
		t.Fatalf("body = %s, want %s", got, wantBody)
	}
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, wantStatus, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var envelope apiErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode API error: %v", err)
	}
	if envelope.Error.Code != wantCode || envelope.Error.Message == "" {
		t.Fatalf("error = %+v, want code %q and safe message", envelope.Error, wantCode)
	}
	assertErrorRequestIDMatchesHeader(t, response)
}

func assertErrorRequestIDMatchesHeader(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	var envelope apiErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode API error: %v", err)
	}
	if got, want := envelope.Error.RequestID, response.Header().Get(requestIDHeader); got == "" || got != want {
		t.Fatalf("error request ID = %q, response header = %q", got, want)
	}
}
