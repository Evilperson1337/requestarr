package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	requestIDHeader    = "X-Request-ID"
	maxRequestIDLength = 64
)

type requestIDContextKey struct{}

var fallbackRequestIDCounter atomic.Uint64

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values(requestIDHeader)
		id := ""
		if len(values) == 1 {
			id = values[0]
		}
		if len(values) != 1 || !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validRequestID(id string) bool {
	if len(id) == 0 || len(id) > maxRequestIDLength {
		return false
	}
	for i := range len(id) {
		c := id[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		// A timestamp fallback keeps request correlation available if the OS random
		// source fails; request IDs are identifiers, not authentication secrets.
		return fmt.Sprintf("%x-%x", time.Now().UnixNano(), fallbackRequestIDCounter.Add(1))
	}
	return hex.EncodeToString(value[:])
}

func requestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDContextKey{}).(string)
	return id
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		if w.status != 0 {
			return
		}
		w.ResponseWriter.WriteHeader(status)
		if status == http.StatusSwitchingProtocols {
			w.status = status
		}
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusWriter) FlushError() error {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func accessLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		response := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(response, r)
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		// Log the registered pattern rather than the caller-controlled URL path.
		// This avoids recording secret-bearing path segments and bounds log cardinality.
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		logger.Info("http request",
			"request_id", requestIDFromContext(r.Context()),
			"method", r.Method,
			"route", route,
			"status", status,
			"duration", time.Since(started),
		)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
