package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/jobs"
	"github.com/evilperson1337/requestarr/internal/notifications"
	"github.com/evilperson1337/requestarr/internal/requests"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
	"github.com/evilperson1337/requestarr/internal/webui"
)

func requireJSONEOF(dec *json.Decoder) error {
	var extra json.RawMessage
	err := dec.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("multiple JSON values")
}

// ReadinessChecker reports whether the application's dependencies (currently
// just the database) are usable. Kept as a small local interface so this
// transport-only package never imports a concrete adapter such as
// internal/store/postgres.
type ReadinessChecker interface {
	Ready(context.Context) error
}

const readyTimeout = 2 * time.Second

// recentRunsPerSource is how many runs the status endpoint returns per Source.
const recentRunsPerSource = 10

func New(logger *slog.Logger, build buildinfo.Info, readiness ReadinessChecker, authHandlers ...*auth.Handler) http.Handler {
	return NewWithMetadata(logger, build, readiness, nil, authHandlers...)
}

func NewWithMetadata(logger *slog.Logger, build buildinfo.Info, readiness ReadinessChecker, resolverService interface {
	ResolveAndUpsert(context.Context, string) (domain.ResolvedItem, error)
}, authHandlers ...*auth.Handler) http.Handler {
	var authHandler *auth.Handler
	if len(authHandlers) > 0 {
		authHandler = authHandlers[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC()})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := readiness.Ready(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	api := http.NewServeMux()
	var requestService *requests.Service
	if backend, ok := readiness.(interface {
		requests.Repository
		Upsert(context.Context, domain.ResolvedItem) (domain.CatalogItem, error)
	}); ok {
		requestService = requests.New(backend)
	}
	if authHandler != nil {
		authHandler.Register(api)
	}
	if inbox, ok := readiness.(notifications.InboxRepository); ok {
		api.HandleFunc("GET /notifications", func(w http.ResponseWriter, r *http.Request) {
			user, err := authHandler.CurrentSession(r)
			if err != nil {
				apierror.Write(w, r, 401, "authentication_required", "authentication required")
				return
			}
			items, err := inbox.ListInbox(r.Context(), user.ID)
			if err != nil {
				apierror.Write(w, r, 500, "notifications_failed", "could not load notifications")
				return
			}
			writeJSON(w, 200, map[string]any{"items": items})
		})
		api.HandleFunc("POST /notifications/{notificationID}/read", func(w http.ResponseWriter, r *http.Request) {
			user, err := authHandler.CurrentSession(r)
			if err != nil {
				apierror.Write(w, r, 401, "authentication_required", "authentication required")
				return
			}
			id := r.PathValue("notificationID")
			if !validUUID(id) {
				apierror.Write(w, r, 400, "invalid_notification_id", "invalid notification ID")
				return
			}
			changed, err := inbox.MarkInboxRead(r.Context(), user.ID, id)
			if err != nil {
				apierror.Write(w, r, 500, "notification_update_failed", "could not update notification")
				return
			}
			if !changed {
				apierror.Write(w, r, 404, "notification_not_found", "notification not found")
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
	api.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, build) })
	api.HandleFunc("GET /setup/status", func(w http.ResponseWriter, r *http.Request) { writeSetupStatus(w, r, readiness) })
	api.HandleFunc("GET /admin/sources/jellyfin/status", func(w http.ResponseWriter, r *http.Request) {
		user, err := authHandler.CurrentSession(r)
		if err != nil {
			apierror.Write(w, r, http.StatusUnauthorized, "authentication_required", "authentication required")
			return
		}
		if user.Role != "admin" {
			apierror.Write(w, r, http.StatusForbidden, "forbidden", "administrator role required")
			return
		}
		statusStore, ok := readiness.(interface {
			JellyfinSyncStatus(context.Context) (domain.SourceSyncStatus, error)
		})
		if !ok {
			apierror.Write(w, r, 503, "source_status_unavailable", "source status unavailable")
			return
		}
		result, err := statusStore.JellyfinSyncStatus(r.Context())
		if err != nil {
			apierror.Write(w, r, 500, "source_status_failed", "could not load source status")
			return
		}
		writeJSON(w, 200, result)
	})
	api.HandleFunc("GET /admin/job-runs", func(w http.ResponseWriter, r *http.Request) {
		user, err := authHandler.CurrentSession(r)
		if err != nil {
			apierror.Write(w, r, http.StatusUnauthorized, "authentication_required", "authentication required")
			return
		}
		if user.Role != "admin" {
			apierror.Write(w, r, http.StatusForbidden, "forbidden", "administrator role required")
			return
		}
		runs, ok := readiness.(interface {
			RecentRunsBySource(context.Context, int) ([]jobs.SourceRuns, error)
		})
		if !ok {
			apierror.Write(w, r, http.StatusServiceUnavailable, "job_runs_unavailable", "job runs unavailable")
			return
		}
		groups, err := runs.RecentRunsBySource(r.Context(), recentRunsPerSource)
		if err != nil {
			apierror.Write(w, r, http.StatusInternalServerError, "job_runs_failed", "could not load job runs")
			return
		}
		if groups == nil {
			groups = []jobs.SourceRuns{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"sources": groups})
	})
	api.HandleFunc("GET /admin/users", func(w http.ResponseWriter, r *http.Request) {
		user, err := authHandler.CurrentSession(r)
		if err != nil {
			apierror.Write(w, r, http.StatusUnauthorized, "authentication_required", "authentication required")
			return
		}
		if user.Role != "admin" {
			apierror.Write(w, r, http.StatusForbidden, "forbidden", "administrator role required")
			return
		}
		store, ok := readiness.(interface {
			ListAdminUsers(context.Context) ([]postgres.AdminUser, error)
		})
		if !ok {
			apierror.Write(w, r, http.StatusServiceUnavailable, "user_management_unavailable", "user management unavailable")
			return
		}
		users, err := store.ListAdminUsers(r.Context())
		if err != nil {
			apierror.Write(w, r, 500, "users_failed", "could not load users")
			return
		}
		writeJSON(w, 200, map[string]any{"users": users})
	})
	api.HandleFunc("PUT /admin/users/{userID}/role", func(w http.ResponseWriter, r *http.Request) {
		user, err := authHandler.CurrentSession(r)
		if err != nil {
			apierror.Write(w, r, 401, "authentication_required", "authentication required")
			return
		}
		if user.Role != "admin" {
			apierror.Write(w, r, 403, "forbidden", "administrator role required")
			return
		}
		id := r.PathValue("userID")
		if !validUUID(id) {
			apierror.Write(w, r, 400, "invalid_user_id", "invalid user ID")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		var input struct {
			Role string `json:"role"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || requireJSONEOF(decoder) != nil || (input.Role != "admin" && input.Role != "requester" && input.Role != "viewer") {
			apierror.Write(w, r, 400, "invalid_role", "role must be viewer, requester or admin")
			return
		}
		store, ok := readiness.(interface {
			ChangeUserRole(context.Context, string, string, string, time.Time) error
		})
		if !ok {
			apierror.Write(w, r, 503, "user_management_unavailable", "user management unavailable")
			return
		}
		err = store.ChangeUserRole(r.Context(), user.ID, id, input.Role, time.Now().UTC())
		switch {
		case errors.Is(err, postgres.ErrAdminSelfRoleChange):
			apierror.Write(w, r, 409, "self_role_change", "cannot change your own role")
		case errors.Is(err, postgres.ErrLastAdmin):
			apierror.Write(w, r, 409, "last_admin", "cannot remove the last administrator")
		case errors.Is(err, postgres.ErrAdminUserNotFound):
			apierror.Write(w, r, 404, "user_not_found", "user not found")
		case err != nil:
			apierror.Write(w, r, 500, "role_change_failed", "could not change user role")
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	api.HandleFunc("GET /requests", func(w http.ResponseWriter, r *http.Request) {
		if requestService == nil {
			apierror.Write(w, r, 503, "requests_unavailable", "requests unavailable")
			return
		}
		user, err := authHandler.CurrentSession(r)
		if err != nil {
			apierror.Write(w, r, 401, "authentication_required", "authentication required")
			return
		}
		mine := false
		if value := r.URL.Query().Get("mine"); value != "" {
			if value != "true" && value != "false" {
				apierror.Write(w, r, http.StatusBadRequest, "invalid_query", "mine must be true or false")
				return
			}
			mine = value == "true"
		}
		items, err := requestService.List(r.Context(), requests.Actor{ID: user.ID, Role: user.Role}, mine)
		if err != nil {
			apierror.Write(w, r, 500, "requests_list_failed", "could not list requests")
			return
		}
		type queueItem struct {
			domain.Request
			CatalogPlatforms []domain.Platform
		}
		out := make([]queueItem, 0, len(items))
		for _, item := range items {
			platforms, platformErr := requestService.CatalogPlatforms(r.Context(), item.CatalogItemID)
			if platformErr != nil {
				apierror.Write(w, r, 500, "request_metadata_failed", "could not load request metadata")
				return
			}
			out = append(out, queueItem{Request: item, CatalogPlatforms: platforms})
		}
		writeJSON(w, 200, map[string]any{"items": out})
	})
	api.HandleFunc("POST /requests/{requestID}/transition", func(w http.ResponseWriter, r *http.Request) {
		if requestService == nil {
			apierror.Write(w, r, 503, "requests_unavailable", "requests unavailable")
			return
		}
		user, err := authHandler.CurrentSession(r)
		if err != nil {
			apierror.Write(w, r, 401, "authentication_required", "authentication required")
			return
		}
		var in struct {
			ToStatus        domain.RequestStatus `json:"toStatus"`
			ExpectedVersion int64                `json:"expectedVersion"`
			OperationID     string               `json:"operationId"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || requireJSONEOF(d) != nil || in.ToStatus == "" || in.ExpectedVersion < 1 || !validUUID(in.OperationID) {
			apierror.Write(w, r, 400, "invalid_transition", "transition body is invalid")
			return
		}
		result, err := requestService.Transition(r.Context(), requests.TransitionInput{Actor: requests.Actor{ID: user.ID, Role: user.Role}, RequestID: r.PathValue("requestID"), ToStatus: in.ToStatus, ExpectedVersion: in.ExpectedVersion, OperationID: in.OperationID})
		if err != nil {
			status, code, message := 500, "transition_failed", "request could not be transitioned"
			switch {
			case errors.Is(err, requests.ErrTransitionForbidden):
				status, code, message = 403, "forbidden", "administrator role required"
			case errors.Is(err, requests.ErrTransitionInvalid):
				status, code, message = 400, "invalid_transition", "requested requests can be approved or denied; open requests can be cancelled"
			case errors.Is(err, requests.ErrTransitionConflict):
				status, code, message = 409, "version_conflict", "request changed; refresh and retry"
			case errors.Is(err, requests.ErrRequestNotFound):
				status, code, message = 404, "request_not_found", "request was not found"
			}
			apierror.Write(w, r, status, code, message)
			return
		}
		writeJSON(w, 200, result)
	})
	for _, method := range []string{"PUT", "DELETE"} {
		method := method
		api.HandleFunc(method+" /requests/{requestID}/subscription", func(w http.ResponseWriter, r *http.Request) {
			if requestService == nil {
				apierror.Write(w, r, 503, "requests_unavailable", "requests unavailable")
				return
			}
			user, err := authHandler.CurrentSession(r)
			if err != nil {
				apierror.Write(w, r, 401, "authentication_required", "authentication required")
				return
			}
			var changed, cancelled bool
			if r.Method == http.MethodPut {
				changed, err = requestService.Subscribe(r.Context(), requests.Actor{ID: user.ID, Role: user.Role}, r.PathValue("requestID"))
			} else {
				changed, cancelled, err = requestService.Unsubscribe(r.Context(), requests.Actor{ID: user.ID, Role: user.Role}, r.PathValue("requestID"), r.URL.Query().Get("confirmCancel") == "true")
			}
			if err != nil {
				status, code, msg := 500, "subscription_failed", "subscription could not be changed"
				if errors.Is(err, requests.ErrRequestNotFound) {
					status, code, msg = 404, "request_not_found", "request was not found"
				} else if errors.Is(err, requests.ErrCancelNeedsConfirmation) {
					status, code, msg = 409, "would_cancel", "you are the last subscriber; leaving cancels the request (retry with confirmCancel=true)"
				} else if errors.Is(err, requests.ErrRequestClosed) {
					status, code, msg = 409, "request_closed", "request is closed to new subscribers"
				} else if errors.Is(err, requests.ErrInvalid) {
					status, code, msg = 400, "invalid_request", "request ID is invalid"
				}
				apierror.Write(w, r, status, code, msg)
				return
			}
			writeJSON(w, 200, map[string]bool{"subscribed": r.Method == http.MethodPut, "changed": changed, "cancelled": cancelled})
		})
	}
	api.HandleFunc("POST /resolve", func(w http.ResponseWriter, r *http.Request) {
		if resolverService == nil {
			apierror.Write(w, r, 503, "metadata_unavailable", "metadata resolution unavailable")
			return
		}
		var in struct {
			URL string `json:"url"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&in); err != nil {
			apierror.Write(w, r, 400, "invalid_json", "invalid JSON body")
			return
		}
		if err := requireJSONEOF(decoder); err != nil {
			apierror.Write(w, r, 400, "invalid_json", "invalid JSON body")
			return
		}
		if in.URL == "" {
			apierror.Write(w, r, http.StatusBadRequest, "invalid_request", "url is required")
			return
		}
		item, err := resolverService.ResolveAndUpsert(r.Context(), in.URL)
		if err != nil {
			apierror.Write(w, r, 400, "metadata_resolution_failed", "metadata URL could not be resolved")
			return
		}
		writeJSON(w, 200, item)
	})
	api.HandleFunc("POST /requests", func(w http.ResponseWriter, r *http.Request) {
		if requestService == nil {
			apierror.Write(w, r, 503, "requests_unavailable", "requests unavailable")
			return
		}
		var in struct {
			URL       string                   `json:"url"`
			CatalogID string                   `json:"catalogId"`
			Note      string                   `json:"note"`
			Platforms domain.PlatformSelection `json:"platforms"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&in); err != nil {
			apierror.Write(w, r, 400, "invalid_json", "invalid JSON body")
			return
		}
		if err := requireJSONEOF(decoder); err != nil {
			apierror.Write(w, r, 400, "invalid_json", "invalid JSON body")
			return
		}
		if in.URL != "" && in.CatalogID != "" {
			apierror.Write(w, r, http.StatusBadRequest, "invalid_request", "provide url or catalogId, not both")
			return
		}
		if in.URL != "" {
			if resolverService == nil {
				apierror.Write(w, r, 503, "metadata_unavailable", "metadata resolution unavailable")
				return
			}
			resolved, resolveErr := resolverService.ResolveAndUpsert(r.Context(), in.URL)
			if resolveErr != nil {
				apierror.Write(w, r, 400, "invalid_metadata_url", "metadata URL could not be resolved")
				return
			}
			in.CatalogID = resolved.Item.ID
		}
		if in.CatalogID == "" {
			apierror.Write(w, r, http.StatusBadRequest, "missing_catalog_id", "catalogId is required when url is omitted")
			return
		}
		user, err := authHandler.CurrentSession(r)
		if err != nil {
			apierror.Write(w, r, 401, "authentication_required", "authentication required")
			return
		}
		result, err := requestService.Create(r.Context(), requests.CreateInput{Actor: requests.Actor{ID: user.ID, Role: user.Role}, CatalogID: in.CatalogID, Note: in.Note, Platforms: in.Platforms})
		if err != nil {
			status := 400
			code := "invalid_request"
			if errors.Is(err, requests.ErrForbidden) {
				status = 403
				code = "forbidden"
			} else if errors.Is(err, requests.ErrCatalogNotFound) {
				status = http.StatusNotFound
				code = "catalog_item_not_found"
			} else if !errors.Is(err, requests.ErrInvalid) {
				status = http.StatusInternalServerError
				code = "request_failed"
			}
			message := "request could not be created"
			if code == "catalog_item_not_found" {
				message = "catalog item was not found"
			}
			if code == "invalid_request" {
				message = "request details are invalid"
			}
			apierror.Write(w, r, status, code, message)
			return
		}
		status := 200
		if result.Created {
			status = 201
		}
		writeJSON(w, status, result)
	})
	// Auth endpoints retain their own method/session/CSRF controls. Product
	// routes and even unknown API paths require a session before dispatch.
	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1")
		if path == "/version" || path == "/setup/status" || (r.Method == http.MethodGet && (path == "/auth/login" || path == "/auth/callback")) {
			http.StripPrefix("/api/v1", routeMisses(api)).ServeHTTP(w, r)
			return
		}
		if authHandler == nil {
			apierror.Write(w, r, http.StatusUnauthorized, "authentication_required", "authentication required")
			return
		}
		if _, err := authHandler.CurrentSession(r); err != nil {
			apierror.Write(w, r, http.StatusUnauthorized, "authentication_required", "authentication required")
			return
		}
		dispatch := http.Handler(http.StripPrefix("/api/v1", routeMisses(api)))
		if protectedMutation(r.Method, path) {
			dispatch = authHandler.CSRFMiddleware(dispatch)
		}
		dispatch.ServeHTTP(w, r)
	})
	mux.Handle("/api/v1/", apiHandler)
	mux.Handle("GET /api/v1/version", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, build) }))
	mux.Handle("GET /api/v1/setup/status", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSetupStatus(w, r, readiness)
	}))
	mux.Handle("/", webui.Handler())

	return apierror.Middleware(requestLogger(logger, securityHeaders(mux)))
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func protectedMutation(method, path string) bool {
	if path == "/resolve" && method == http.MethodPost || path == "/requests" && method == http.MethodPost || path == "/setup/claim-admin" && method == http.MethodPost || strings.HasPrefix(path, "/admin/users/") && strings.HasSuffix(path, "/role") && method == http.MethodPut || strings.HasPrefix(path, "/requests/") && strings.HasSuffix(path, "/transition") && method == http.MethodPost || strings.HasPrefix(path, "/notifications/") && strings.HasSuffix(path, "/read") && method == http.MethodPost {
		return true
	}
	return strings.HasPrefix(path, "/requests/") && strings.HasSuffix(path, "/subscription") && (method == http.MethodPut || method == http.MethodDelete)
}

func writeSetupStatus(w http.ResponseWriter, r *http.Request, readiness ReadinessChecker) {
	if state, ok := readiness.(interface {
		SetupComplete(context.Context) (bool, error)
	}); ok {
		complete, err := state.SetupComplete(r.Context())
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]bool{"complete": false, "required": true})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"complete": complete, "required": !complete})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"complete": false, "required": true})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Debug("http request", "request_id", apierror.RequestID(r.Context()), "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(started))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

// routeMisses turns the mux's plain-text 404/405 replies into the standard
// error body, keeping any Allow header the mux set.
func routeMisses(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r) // ServeHTTP, not handler, so path values are set
			return
		}
		probe := &missProbe{header: w.Header()}
		handler.ServeHTTP(probe, r)
		if probe.status == http.StatusMethodNotAllowed {
			apierror.Write(w, r, probe.status, "method_not_allowed", "method not allowed")
			return
		}
		apierror.Write(w, r, http.StatusNotFound, "not_found", "resource not found")
	})
}

type missProbe struct {
	header http.Header
	status int
}

func (m *missProbe) Header() http.Header         { return m.header }
func (m *missProbe) Write(b []byte) (int, error) { return len(b), nil }
func (m *missProbe) WriteHeader(status int)      { m.status = status }

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
