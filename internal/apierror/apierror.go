// Package apierror is the single place API failures are written, so every
// error response has one JSON shape and carries the request's correlation ID.
package apierror

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// Header carries the request ID on every response.
const Header = "X-Request-Id"

type ctxKey struct{}

// RequestID returns the ID assigned by Middleware, or "" outside a request.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// Middleware assigns each request a fresh ID, exposes it on the response
// header and in the request context. Client-supplied IDs are never trusted.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newID()
		w.Header().Set(Header, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Write sends the standard error body: {"error":{"code","message","requestId"}}.
func Write(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
		"code":      code,
		"message":   message,
		"requestId": RequestID(r.Context()),
	}})
}
