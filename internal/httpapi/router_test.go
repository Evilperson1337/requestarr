package httpapi

import (
	"context"
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

func testHandler(checker ReadinessChecker) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(logger, buildinfo.Info{}, checker)
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
