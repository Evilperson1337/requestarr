package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/buildinfo"
	"git.example.com/evilperson1337/Requestarr/internal/setup"
)

const (
	readinessTimeout    = 2 * time.Second
	setupStatusTimeout  = 2 * time.Second
	setupSessionTimeout = 2 * time.Second
	setupSessionBodyMax = int64(128)
)

// ReadinessChecker is owned by the HTTP consumer and hides concrete database details.
type ReadinessChecker interface {
	Ready(context.Context) error
}

// SetupStatusReader exposes only the public setup completion signal.
type SetupStatusReader interface {
	Complete(context.Context) (bool, error)
}

type SetupSessionExchanger interface {
	Exchange(context.Context, string, netip.Addr) setup.BootstrapResult
}

type SetupWorkflow interface {
	State(context.Context, string) setup.WorkflowResult
	PutGeneral(context.Context, string, setup.General) setup.WorkflowResult
}

type SetupSessionOptions struct {
	Exchanger    SetupSessionExchanger
	Workflow     SetupWorkflow
	PublicOrigin string
	SecureCookie bool
}

type probeResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type setupStatusResponse struct {
	Complete bool `json:"complete"`
}

func New(logger *slog.Logger, build buildinfo.Info, readiness ReadinessChecker, setupStatus SetupStatusReader, setupSession ...SetupSessionOptions) http.Handler {
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
	var sessionOptions SetupSessionOptions
	if len(setupSession) == 1 {
		sessionOptions = setupSession[0]
	}
	mux.Handle("/api/v1/setup/session", noStore(setupSessionHandler(sessionOptions)))
	mux.Handle("/api/v1/setup/state", noStore(setupStateHandler(sessionOptions)))
	mux.Handle("/api/v1/setup/general", noStore(setupGeneralHandler(sessionOptions)))
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

type setupSessionRequest struct {
	Token string `json:"token"`
}

func (request *setupSessionRequest) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("setup session document must be an object")
	}
	seen := false
	for decoder.More() {
		name, err := decoder.Token()
		if err != nil || name != "token" || seen {
			return errors.New("setup session document must contain exactly one token field")
		}
		if err := decoder.Decode(&request.Token); err != nil {
			return errors.New("setup session token must be a string")
		}
		seen = true
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !seen {
		return errors.New("setup session document must contain exactly one token field")
	}
	return nil
}

func setupSessionHandler(options SetupSessionOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r, http.MethodPost)
			return
		}
		origins := r.Header.Values("Origin")
		if len(origins) != 1 {
			writeAPIError(w, r, http.StatusForbidden, "setup_origin_forbidden", "The request origin is not allowed.")
			return
		}
		origin, err := setup.NormalizeOrigin(origins[0])
		if err != nil || origin != options.PublicOrigin {
			writeAPIError(w, r, http.StatusForbidden, "setup_origin_forbidden", "The request origin is not allowed.")
			return
		}
		contentTypes := r.Header.Values("Content-Type")
		if len(contentTypes) != 1 || contentTypes[0] != "application/json" {
			writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		var request setupSessionRequest
		if err := decodeJSON(w, r, &request, setupSessionBodyMax, false); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		if _, err := setup.DecodeBootstrapToken(request.Token); err != nil {
			writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		peer, err := setup.DirectPeerIP(r.RemoteAddr)
		if err != nil || options.Exchanger == nil {
			writeAPIError(w, r, http.StatusServiceUnavailable, "setup_unavailable", "Setup is temporarily unavailable.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), setupSessionTimeout)
		defer cancel()
		result := options.Exchanger.Exchange(ctx, request.Token, peer)
		switch result.Outcome {
		case setup.BootstrapSessionCreated:
			cookieName := "__Host-requestarr_setup"
			if !options.SecureCookie {
				cookieName = "requestarr_setup_dev"
			}
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    result.Session,
				Path:     "/",
				Expires:  result.ExpiresAt,
				MaxAge:   int(setup.SessionMaxAge / time.Second),
				Secure:   options.SecureCookie,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			w.WriteHeader(http.StatusNoContent)
		case setup.BootstrapTokenInvalid:
			writeAPIError(w, r, http.StatusUnauthorized, "setup_token_invalid", "The setup token is invalid.")
		case setup.BootstrapRateLimited:
			seconds := int64(result.RetryAfter / time.Second)
			if result.RetryAfter%time.Second != 0 {
				seconds++
			}
			if seconds < 1 {
				seconds = 1
			}
			if seconds > 900 {
				seconds = 900
			}
			w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
			writeAPIError(w, r, http.StatusTooManyRequests, "setup_rate_limited", "Too many setup attempts. Try again later.")
		case setup.BootstrapSetupCompleted:
			writeAPIError(w, r, http.StatusConflict, "setup_completed", "Setup has already been completed.")
		case setup.BootstrapSetupLocked:
			writeAPIError(w, r, http.StatusLocked, "setup_locked", "Setup is locked by server configuration.")
		default:
			writeAPIError(w, r, http.StatusServiceUnavailable, "setup_unavailable", "Setup is temporarily unavailable.")
		}
	})
}
