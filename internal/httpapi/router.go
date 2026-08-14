package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/requestarr/internal/buildinfo"
)

func New(logger *slog.Logger, build buildinfo.Info) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC()})
	})
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, build)
	})
	mux.HandleFunc("GET /api/v1/setup/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"complete": false, "scaffold": true})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>Requestarr</title><h1>Requestarr scaffold</h1><p>Run the React UI from <code>web/</code> during development.</p>`))
	})

	return requestLogger(logger, securityHeaders(mux))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.Debug("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
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
