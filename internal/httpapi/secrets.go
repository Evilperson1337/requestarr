package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

// SecretsService manages stored integration secrets (D024). It is write-only:
// nothing here returns a stored value. The router uses it when the setup
// service also implements it.
type SecretsService interface {
	Status(context.Context) ([]secrets.Status, bool, error)
	Set(ctx context.Context, actorID string, name secrets.Name, value string) error
	Clear(ctx context.Context, actorID string, name secrets.Name) error
}

type secretStatusBody struct {
	Name           string `json:"name"`
	Label          string `json:"label"`
	EnvVar         string `json:"envVar"`
	Configured     bool   `json:"configured"`
	Source         string `json:"source"`
	Unreadable     bool   `json:"unreadable"`
	PendingRestart bool   `json:"pendingRestart"`
}

func registerSecretsRoutes(rt *routeTable, service SecretsService) {
	admin := func(pattern string, next func(http.ResponseWriter, *http.Request, string)) {
		rt.handle(pattern, accessSuperuser, func(w http.ResponseWriter, r *http.Request, user auth.User) {
			w.Header().Set("Cache-Control", "no-store")
			next(w, r, user.ID)
		})
	}
	fail := func(w http.ResponseWriter, r *http.Request, err error) {
		switch {
		case errors.Is(err, secrets.ErrUnknownSecret):
			apierror.Write(w, r, 404, "unknown_secret", "unknown secret")
		case errors.Is(err, secrets.ErrNoMasterKey):
			apierror.Write(w, r, 409, "secrets_disabled", "REQUESTARR_SECRETS_KEY is not configured, so secret values cannot be saved")
		case errors.Is(err, secrets.ErrInvalidValue):
			apierror.Write(w, r, 422, "invalid_secret_value", "the value must be non-empty, at most 4096 characters, without surrounding whitespace or control characters")
		case errors.Is(err, ErrNotApplied):
			apierror.Write(w, r, 500, "settings_not_applied", "the change was saved but could not be applied; the previous configuration is still in use")
		default:
			// Storage errors can carry connection details; never echo them.
			apierror.Write(w, r, 500, "secret_update_failed", "could not update the stored secret")
		}
	}
	admin("GET /admin/secrets", func(w http.ResponseWriter, r *http.Request, _ string) {
		statuses, enabled, err := service.Status(r.Context())
		if err != nil {
			apierror.Write(w, r, 500, "secret_status_failed", "could not load secret status")
			return
		}
		out := make([]secretStatusBody, 0, len(statuses))
		for _, s := range statuses {
			if managed(s.Name) {
				continue
			}
			out = append(out, secretStatusBody{Name: string(s.Name), Label: s.Label, EnvVar: s.EnvVar, Configured: s.Configured, Source: string(s.Source), Unreadable: s.Unreadable, PendingRestart: s.PendingRestart})
		}
		writeJSON(w, 200, map[string]any{"enabled": enabled, "secrets": out})
	})
	admin("PUT /admin/secrets/{name}", func(w http.ResponseWriter, r *http.Request, actorID string) {
		if managed(secrets.Name(r.PathValue("name"))) {
			apierror.Write(w, r, 409, "oidc_settings_required", "manage the OIDC secret through Authentication settings so discovery and metadata apply atomically")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var input struct {
			Value string `json:"value"`
		}
		if decoder.Decode(&input) != nil || requireJSONEOF(decoder) != nil {
			apierror.Write(w, r, 400, "invalid_secret_input", "provide one JSON object with a value field")
			return
		}
		if err := service.Set(r.Context(), actorID, secrets.Name(r.PathValue("name")), input.Value); err != nil {
			fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	admin("DELETE /admin/secrets/{name}", func(w http.ResponseWriter, r *http.Request, actorID string) {
		if managed(secrets.Name(r.PathValue("name"))) {
			apierror.Write(w, r, 409, "oidc_settings_required", "discard saved Authentication settings to remove the OIDC secret atomically")
			return
		}
		if err := service.Clear(r.Context(), actorID, secrets.Name(r.PathValue("name"))); err != nil {
			fail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// managed reports whether another settings screen owns the secret (see
// secrets.Spec.Managed).
func managed(name secrets.Name) bool {
	spec, ok := secrets.Lookup(name)
	return ok && spec.Managed
}
