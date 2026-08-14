package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/buildinfo"
)

const (
	readinessTimeout   = 2 * time.Second
	setupStatusTimeout = 2 * time.Second
)

// ReadinessChecker is owned by the HTTP consumer and hides concrete database details.
type ReadinessChecker interface {
	Ready(context.Context) error
}

// SetupStatusReader exposes only the public setup completion signal.
type SetupStatusReader interface {
	Complete(context.Context) (bool, error)
}

type probeResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type setupStatusResponse struct {
	Complete bool `json:"complete"`
}

func New(logger *slog.Logger, build buildinfo.Info, readiness ReadinessChecker, setupStatus SetupStatusReader) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, probeResponse{Status: "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()
		if readiness == nil || readiness.Ready(ctx) != nil {
			writeJSON(w, http.StatusServiceUnavailable, probeResponse{Status: "not_ready", Reason: "database_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, probeResponse{Status: "ready"})
	})
	mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeMethodNotAllowed(w, r, http.MethodGet, http.MethodHead)
			return
		}
		writeJSON(w, http.StatusOK, build)
	})
	mux.Handle("/api/v1/setup/status", noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeMethodNotAllowed(w, r, http.MethodGet, http.MethodHead)
			return
		}
		if setupStatus == nil {
			writeAPIError(w, r, http.StatusServiceUnavailable, "setup_status_unavailable", "Setup status is temporarily unavailable.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), setupStatusTimeout)
		defer cancel()
		complete, err := setupStatus.Complete(ctx)
		if err != nil {
			writeAPIError(w, r, http.StatusServiceUnavailable, "setup_status_unavailable", "Setup status is temporarily unavailable.")
			return
		}
		writeJSON(w, http.StatusOK, setupStatusResponse{Complete: complete})
	})))
	mux.HandleFunc("/api/v1", func(w http.ResponseWriter, r *http.Request) {
		writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested API resource was not found.")
	})
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested API resource was not found.")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>Requestarr</title><h1>Requestarr scaffold</h1><p>Run the React UI from <code>web/</code> during development.</p>`))
	})

	return requestID(securityHeaders(accessLogger(logger, mux)))
}
