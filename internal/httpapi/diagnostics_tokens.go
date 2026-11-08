package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
)

// registerDiagnosticsTokenRoutes lets a superuser make, list and revoke
// diagnostics tokens (D043). These routes are deliberately not marked
// token-readable: a token can never create or revoke tokens. Every response is
// no-store, and the token value appears only in the creation response.
// maxDiagnosticsTokenBody bounds the create request body.
const maxDiagnosticsTokenBody = 4 << 10

const hoursPerDay = 24 * time.Hour

func registerDiagnosticsTokenRoutes(rt *routeTable) {
	service := func(w http.ResponseWriter, r *http.Request) *auth.DiagnosticsTokens {
		if rt.auth == nil || rt.auth.Diagnostics == nil {
			apierror.Write(w, r, http.StatusServiceUnavailable, "diagnostics_tokens_unavailable", "diagnostics tokens are not available")
			return nil
		}
		return rt.auth.Diagnostics
	}
	fail := func(w http.ResponseWriter, r *http.Request, err error) {
		switch {
		case errors.Is(err, auth.ErrDiagnosticsTokenName), errors.Is(err, auth.ErrDiagnosticsTokenLifetime):
			apierror.Write(w, r, http.StatusUnprocessableEntity, "invalid_diagnostics_token", err.Error())
		case errors.Is(err, auth.ErrDiagnosticsTokenLimit):
			apierror.Write(w, r, http.StatusConflict, "diagnostics_token_limit", err.Error())
		case errors.Is(err, auth.ErrDiagnosticsTokenNotFound):
			apierror.Write(w, r, http.StatusNotFound, "diagnostics_token_not_found", "diagnostics token not found")
		case errors.Is(err, auth.ErrDiagnosticsTokenForbidden):
			apierror.Write(w, r, http.StatusForbidden, "forbidden", messageSuperuserRequired)
		default:
			// Storage errors can carry connection details; never echo them.
			apierror.Write(w, r, http.StatusInternalServerError, "diagnostics_token_failed", "could not complete the diagnostics token request")
		}
	}
	rt.handle("GET /admin/diagnostics-tokens", accessSuperuser, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		svc := service(w, r)
		if svc == nil {
			return
		}
		tokens, err := svc.List(r.Context(), user)
		if err != nil {
			fail(w, r, err)
			return
		}
		if tokens == nil {
			tokens = []auth.DiagnosticsToken{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens, "maxActive": auth.MaxActiveDiagnosticsTokens})
	})
	rt.handle("POST /admin/diagnostics-tokens", accessSuperuser, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		svc := service(w, r)
		if svc == nil {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxDiagnosticsTokenBody)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var in struct {
			Name         string `json:"name"`
			LifetimeDays *int   `json:"lifetimeDays"`
		}
		if decoder.Decode(&in) != nil || requireJSONEOF(decoder) != nil {
			apierror.Write(w, r, http.StatusBadRequest, "invalid_diagnostics_token_input", "provide one JSON object with a name and optionally lifetimeDays")
			return
		}
		var lifetime time.Duration
		if in.LifetimeDays != nil {
			if *in.LifetimeDays < 0 || *in.LifetimeDays > int(auth.DiagnosticsTokenMaxLifetime/hoursPerDay) {
				// Out of range: the service names the error; this keeps the
				// multiplication from overflowing.
				lifetime = -1
			} else {
				lifetime = time.Duration(*in.LifetimeDays) * hoursPerDay
			}
		}
		value, token, err := svc.Create(r.Context(), user, in.Name, lifetime, rt.auth.ClientAddr(r))
		if err != nil {
			fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"value": value, "token": token})
	})
	rt.handle("DELETE /admin/diagnostics-tokens/{id}", accessSuperuser, func(w http.ResponseWriter, r *http.Request, user auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		svc := service(w, r)
		if svc == nil {
			return
		}
		id := r.PathValue("id")
		if !validUUID(id) {
			// The store keys on a uuid column; a malformed id is simply not a token.
			fail(w, r, auth.ErrDiagnosticsTokenNotFound)
			return
		}
		if err := svc.Revoke(r.Context(), user, id, rt.auth.ClientAddr(r)); err != nil {
			fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
