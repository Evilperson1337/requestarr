package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/requestarr/internal/buildinfo"
)

type fakeReadiness struct {
	err error
}

func (f fakeReadiness) Ready(context.Context) error { return f.err }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealthzAlwaysOK(t *testing.T) {
	handler := New(testLogger(), buildinfo.Info{}, fakeReadiness{err: errors.New("db down")})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadyzReflectsChecker(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "ready", err: nil, want: http.StatusOK},
		{name: "not ready", err: errors.New("db down"), want: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := New(testLogger(), buildinfo.Info{}, fakeReadiness{err: tt.err})

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if rec.Code != tt.want {
				t.Fatalf("readyz status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
