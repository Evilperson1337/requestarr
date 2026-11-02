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
	"github.com/evilperson1337/requestarr/internal/devlogs"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/jobs"
	"github.com/evilperson1337/requestarr/internal/notifications"
	"github.com/evilperson1337/requestarr/internal/opstatus"
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

// SourceSyncer starts a manual sync of a library Source, named by its route
// slug (for example "jellyfin"), and returns the run it started. It returns
// jobs.ErrAlreadyRunning while a run is active and jobs.ErrUnknownSource for a
// Source that is not configured.
type SourceSyncer interface {
	SyncNow(ctx context.Context, source string) (jobs.Run, error)
}

// Resolver turns a pasted metadata URL into a canonical catalog item.
type Resolver interface {
	ResolveAndUpsert(context.Context, string) (domain.ResolvedItem, error)
}

// JobRuns lists recent sync runs per Source.
type JobRuns interface {
	RecentRunsBySource(ctx context.Context, perSource int) ([]jobs.SourceRuns, error)
}

// StatusSource is the operational status overview.
type StatusSource interface {
	Overview(context.Context) (opstatus.Overview, error)
}

// UserAdmin lists users and changes their roles.
type UserAdmin interface {
	ListAdminUsers(context.Context) ([]postgres.AdminUser, error)
	ChangeUserRole(ctx context.Context, actorID, targetID, role string, at time.Time) error
}

// SetupState reports whether first-run setup and the wizard are finished.
type SetupState interface {
	SetupComplete(context.Context) (bool, error)
	SetupWizardComplete(context.Context) (bool, error)
}

// Deps is everything the HTTP API needs. Every field except DevelopmentLogs is
// required: NewRouter fails at startup naming what is missing, instead of
// serving a route that answers 503 later.
type Deps struct {
	Logger    *slog.Logger
	Build     buildinfo.Info
	Readiness ReadinessChecker
	Auth      *auth.Handler

	Requests     *requests.Service
	Resolver     Resolver
	MoviePreview MoviePreviewReader
	Inbox        notifications.InboxRepository
	MatchReview  MatchReviewStore
	JobRuns      JobRuns
	Status       StatusSource
	Users        UserAdmin
	SetupState   SetupState
	Syncer       SourceSyncer
	Wizard       SetupService
	Secrets      SecretsService
	Connections  ConnectionsService
	Library      LibraryReader

	// DevelopmentLogs is nil unless the development-only flag enabled it.
	DevelopmentLogs *devlogs.Recorder
}

// missing names the required dependencies that are nil.
func (d Deps) missing() []string {
	var names []string
	for _, f := range []struct {
		name string
		nil  bool
	}{
		{"Logger", d.Logger == nil}, {"Readiness", d.Readiness == nil}, {"Auth", d.Auth == nil},
		{"Requests", d.Requests == nil}, {"Resolver", d.Resolver == nil}, {"Inbox", d.Inbox == nil},
		{"MoviePreview", d.MoviePreview == nil},
		{"MatchReview", d.MatchReview == nil}, {"JobRuns", d.JobRuns == nil}, {"Status", d.Status == nil},
		{"Users", d.Users == nil}, {"SetupState", d.SetupState == nil}, {"Syncer", d.Syncer == nil},
		{"Wizard", d.Wizard == nil}, {"Secrets", d.Secrets == nil}, {"Connections", d.Connections == nil}, {"Library", d.Library == nil},
	} {
		if f.nil {
			names = append(names, f.name)
		}
	}
	return names
}

// NewRouter builds the API and UI handler. It fails when a required
// dependency is missing.
func NewRouter(d Deps) (http.Handler, error) {
	handler, _, err := buildRouter(d)
	return handler, err
}

// buildRouter also returns the route table, which tests read to check the access
// and CSRF policy of every route.
func buildRouter(d Deps) (http.Handler, *routeTable, error) {
	if missing := d.missing(); len(missing) > 0 {
		return nil, nil, fmt.Errorf("httpapi: missing dependencies: %s", strings.Join(missing, ", "))
	}
	logger, buildInfo, readiness, authHandler := d.Logger, d.Build, d.Readiness, d.Auth
	requestService, resolverService, syncer, wizard := d.Requests, d.Resolver, d.Syncer, d.Wizard
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
	rt := newRouteTable(api, authHandler)
	logs := d.DevelopmentLogs
	registerDevelopmentLogs(rt, logs)
	{
		authHandler.Register(api)
		rt.record("GET /auth/login", accessPublic, false)
		rt.record("GET /auth/callback", accessPublic, false)
		rt.record("POST /auth/logout", accessSession, false) // checks CSRF itself
		rt.record("GET /auth/me", accessSession, false)
		rt.record("POST /setup/claim-admin", accessSession, true)
	}
	if inbox := d.Inbox; inbox != nil {
		rt.handle("GET /notifications", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
			items, err := inbox.ListInbox(r.Context(), user.ID)
			if err != nil {
				apierror.Write(w, r, 500, "notifications_failed", "could not load notifications")
				return
			}
			writeJSON(w, 200, map[string]any{"items": items})
		})
		rt.handle("POST /notifications/{notificationID}/read", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
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
	rt.handle("GET /version", accessPublic, func(w http.ResponseWriter, r *http.Request, _ auth.User) { writeJSON(w, http.StatusOK, buildInfo) })
	registerMatchReviewRoutes(rt, d.MatchReview)
	registerSetupRoutes(rt, wizard)
	registerSecretsRoutes(rt, d.Secrets)
	registerConnectionRoutes(rt, d.Connections)
	registerLibraryRoutes(rt, d.Library)
	rt.handle("GET /setup/status", accessPublic, func(w http.ResponseWriter, r *http.Request, _ auth.User) { writeSetupStatus(w, r, d.SetupState) })
	rt.handle("POST /admin/sources/{source}/sync", accessAdmin, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		run, err := syncer.SyncNow(r.Context(), r.PathValue("source"))
		switch {
		case errors.Is(err, jobs.ErrAlreadyRunning):
			apierror.Write(w, r, http.StatusConflict, "sync_in_progress", "a sync for this source is already running")
		case errors.Is(err, jobs.ErrUnknownSource):
			apierror.Write(w, r, http.StatusNotFound, "source_not_configured", "source is not configured")
		case err != nil:
			apierror.Write(w, r, http.StatusInternalServerError, "sync_failed", "could not start sync")
		default:
			writeJSON(w, http.StatusAccepted, map[string]any{"run": run})
		}
	})
	rt.handle("GET /admin/job-runs", accessAdmin, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		groups, err := d.JobRuns.RecentRunsBySource(r.Context(), recentRunsPerSource)
		if err != nil {
			apierror.Write(w, r, http.StatusInternalServerError, "job_runs_failed", "could not load job runs")
			return
		}
		if groups == nil {
			groups = []jobs.SourceRuns{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"sources": groups})
	})
	rt.handle("GET /admin/status", accessAdmin, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		overview, err := d.Status.Overview(r.Context())
		if err != nil {
			apierror.Write(w, r, http.StatusInternalServerError, "status_failed", "could not load operational status")
			return
		}
		overview.DevelopmentLogsEnabled = logs != nil
		writeJSON(w, http.StatusOK, overview)
	})
	rt.handle("GET /admin/users", accessAdmin, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		users, err := d.Users.ListAdminUsers(r.Context())
		if err != nil {
			apierror.Write(w, r, 500, "users_failed", "could not load users")
			return
		}
		writeJSON(w, 200, map[string]any{"users": users})
	})
	rt.handle("PUT /admin/users/{userID}/role", accessAdmin, func(w http.ResponseWriter, r *http.Request, user auth.User) {
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
		err := d.Users.ChangeUserRole(r.Context(), user.ID, id, input.Role, time.Now().UTC())
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
	rt.handle("GET /requests", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		query, err := parseRequestListQuery(r.URL.RawQuery)
		if err != nil {
			writeRequestListError(w, r, err)
			return
		}
		page, err := requestService.List(r.Context(), requests.Actor{ID: user.ID, Role: user.Role}, query)
		if err != nil {
			writeRequestListError(w, r, err)
			return
		}
		type queueItem struct {
			domain.Request
			CatalogPlatforms []domain.Platform
		}
		out := make([]queueItem, 0, len(page.Items))
		for _, item := range page.Items {
			platforms, platformErr := requestService.CatalogPlatforms(r.Context(), item.CatalogItemID)
			if platformErr != nil {
				apierror.Write(w, r, 500, "request_metadata_failed", "could not load request metadata")
				return
			}
			out = append(out, queueItem{Request: item, CatalogPlatforms: platforms})
		}
		writeJSON(w, 200, map[string]any{"items": out, "nextCursor": page.NextCursor})
	})
	rt.handle("POST /requests/{requestID}/transition", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
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
		rt.handle(method+" /requests/{requestID}/subscription", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
			var err error
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
	rt.handle("GET /catalog/{catalogID}/preview", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		id := r.PathValue("catalogID")
		if !domain.ValidUUID(id) {
			apierror.Write(w, r, 400, "invalid_catalog_id", "catalog ID is invalid")
			return
		}
		state, err := moviePreview(r.Context(), d.MoviePreview, id, user)
		if err != nil {
			writeMoviePreviewError(w, r, err)
			return
		}
		writeJSON(w, 200, state)
	})
	rt.handle("POST /resolve", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
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
			// Resolver errors carry no credentials: those travel in headers and bodies.
			writeResolveError(w, r, logger, err, "metadata_resolution_failed")
			return
		}
		// Every media type previews its request and availability state alike.
		state, err := moviePreview(r.Context(), d.MoviePreview, item.Item.ID, user)
		if err != nil {
			writeMoviePreviewError(w, r, err)
			return
		}
		item.Preview = &state
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, item)
	})
	rt.handle("POST /requests", accessSession, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		var in struct {
			URL       string                   `json:"url"`
			CatalogID string                   `json:"catalogId"`
			Note      string                   `json:"note"`
			Platforms domain.PlatformSelection `json:"platforms"`
			Selection domain.Selection         `json:"selection"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
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
			resolved, resolveErr := resolverService.ResolveAndUpsert(r.Context(), in.URL)
			if resolveErr != nil {
				writeResolveError(w, r, logger, resolveErr, "invalid_metadata_url")
				return
			}
			in.CatalogID = resolved.Item.ID
		}
		if in.CatalogID == "" {
			apierror.Write(w, r, http.StatusBadRequest, "missing_catalog_id", "catalogId is required when url is omitted")
			return
		}
		result, err := requestService.Create(r.Context(), requests.CreateInput{Actor: requests.Actor{ID: user.ID, Role: user.Role}, CatalogID: in.CatalogID, Note: in.Note, Platforms: in.Platforms, Selection: in.Selection})
		if err != nil {
			writeCreateRequestError(w, r, err)
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
		if path == "/admin/development-logs" || path == "/admin/status" {
			// Apply before the session gate, including denied and disabled requests.
			w.Header().Set("Cache-Control", "no-store")
		}
		policy, known := rt.policyFor(r, path)
		if known && policy.Access == accessPublic {
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
		if known && policy.CSRF {
			dispatch = authHandler.CSRFMiddleware(dispatch)
		}
		dispatch.ServeHTTP(w, r)
	})
	mux.Handle("/api/v1/", apiHandler)
	mux.Handle("GET /api/v1/version", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, buildInfo) }))
	mux.Handle("GET /api/v1/setup/status", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSetupStatus(w, r, d.SetupState)
	}))
	mux.Handle("/", webui.Handler())

	return apierror.Middleware(requestLogger(logger, securityHeaders(mux))), rt, nil
}

// writeCreateRequestError maps a failed request creation to its HTTP status.
func writeCreateRequestError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := http.StatusBadRequest, "invalid_request", "request details are invalid"
	switch {
	case errors.Is(err, requests.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "request could not be created"
	case errors.Is(err, requests.ErrCatalogNotFound):
		status, code, message = http.StatusNotFound, "catalog_item_not_found", "catalog item was not found"
	case !errors.Is(err, requests.ErrInvalid):
		status, code, message = http.StatusInternalServerError, "request_failed", "request could not be created"
	}
	apierror.Write(w, r, status, code, message)
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

func writeSetupStatus(w http.ResponseWriter, r *http.Request, state SetupState) {
	complete, err := state.SetupComplete(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]bool{"complete": false, "required": true})
		return
	}
	wizardComplete := false
	if complete {
		// A lookup failure reads as "not finished"; the wizard stays reachable.
		wizardComplete, _ = state.SetupWizardComplete(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]bool{"complete": complete, "required": !complete, "wizardComplete": wizardComplete})
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
