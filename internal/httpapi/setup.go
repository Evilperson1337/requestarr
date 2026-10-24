package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/setup"
)

type SetupService interface {
	Load(context.Context, setup.Actor) (setup.Draft, error)
	Save(context.Context, setup.Actor, setup.SaveInput) (setup.Draft, error)
	Validate(context.Context, setup.Actor, int64, setup.Target) (setup.Draft, error)
	Finish(context.Context, setup.Actor, int64) (setup.Draft, error)
}

func registerSetupRoutes(api *http.ServeMux, service SetupService, authHandler *auth.Handler) {
	admin := func(next func(http.ResponseWriter, *http.Request, setup.Actor)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			user, err := authHandler.CurrentSession(r)
			if err != nil {
				apierror.Write(w, r, 401, "authentication_required", "authentication required")
				return
			}
			if user.Role != "admin" {
				apierror.Write(w, r, 403, "forbidden", "administrator role required")
				return
			}
			if service == nil {
				apierror.Write(w, r, 503, "setup_unavailable", "setup draft unavailable")
				return
			}
			next(w, r, setup.Actor{ID: user.ID, Role: user.Role})
		}
	}
	respond := func(w http.ResponseWriter, r *http.Request, draft setup.Draft, err error) {
		switch {
		case errors.Is(err, setup.ErrForbidden):
			apierror.Write(w, r, 403, "forbidden", "administrator role required")
		case errors.Is(err, setup.ErrInvalid):
			apierror.Write(w, r, 422, "invalid_setup_settings", "check the non-secret fields, URLs, timezone, and permitted environment-variable names")
		case errors.Is(err, setup.ErrCompleted):
			apierror.Write(w, r, 409, "setup_already_complete", "setup is already complete; the wizard no longer accepts changes")
		case errors.Is(err, setup.ErrNotReady):
			apierror.Write(w, r, 422, "setup_not_ready", "every required connection check must pass for the saved revision before finishing")
		case errors.Is(err, setup.ErrConflict):
			apierror.Write(w, r, 409, "setup_revision_conflict", "setup draft changed; reload the saved draft before retrying")
		case err != nil:
			apierror.Write(w, r, 500, "setup_failed", "could not update or load setup draft")
		default:
			writeJSON(w, 200, draft)
		}
	}
	decode := func(w http.ResponseWriter, r *http.Request, input any) bool {
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(input) != nil || requireJSONEOF(decoder) != nil {
			apierror.Write(w, r, 400, "invalid_setup_input", "provide one JSON object with supported non-secret setup fields only")
			return false
		}
		return true
	}
	api.HandleFunc("GET /admin/setup/draft", admin(func(w http.ResponseWriter, r *http.Request, actor setup.Actor) {
		draft, err := service.Load(r.Context(), actor)
		respond(w, r, draft, err)
	}))
	api.HandleFunc("PUT /admin/setup/draft", admin(func(w http.ResponseWriter, r *http.Request, actor setup.Actor) {
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		body, err := io.ReadAll(r.Body)
		if err != nil || !hasRequiredDraftFields(body) {
			apierror.Write(w, r, 400, "invalid_setup_input", "provide every setup draft field; values may be empty where allowed")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var input setup.SaveInput
		if !decode(w, r, &input) {
			return
		}
		draft, err := service.Save(r.Context(), actor, input)
		respond(w, r, draft, err)
	}))
	api.HandleFunc("POST /admin/setup/validate", admin(func(w http.ResponseWriter, r *http.Request, actor setup.Actor) {
		var input struct {
			ExpectedRevision int64        `json:"expectedRevision"`
			Target           setup.Target `json:"target"`
		}
		if !decode(w, r, &input) {
			return
		}
		draft, err := service.Validate(r.Context(), actor, input.ExpectedRevision, input.Target)
		respond(w, r, draft, err)
	}))
	api.HandleFunc("POST /admin/setup/finish", admin(func(w http.ResponseWriter, r *http.Request, actor setup.Actor) {
		var input struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if !decode(w, r, &input) {
			return
		}
		draft, err := service.Finish(r.Context(), actor, input.ExpectedRevision)
		respond(w, r, draft, err)
	}))
}

func hasRequiredDraftFields(body []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return false
	}
	settings, ok := objectFields(fields["settings"])
	if !ok {
		return false
	}
	for section, required := range map[string][]string{
		"general":        {"instanceName", "timezone", "publicUrl"},
		"authentication": {"issuer", "clientId", "clientSecretEnv"},
		"providers":      {"tmdbEnabled", "tmdbTokenEnv", "musicBrainzEnabled", "musicBrainzContact", "igdbEnabled", "igdbClientId", "igdbClientSecretEnv"},
	} {
		sectionFields, ok := objectFields(settings[section])
		if !ok {
			return false
		}
		for _, key := range required {
			value, exists := sectionFields[key]
			if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return false
			}
		}
	}
	for section, required := range map[string][]string{
		"notifications": {"appriseEnabled", "appriseUrl", "appriseConfigKeyEnv"},
		"policy":        {"autoApproveMediaTypes"},
	} {
		sectionFields, ok := objectFields(settings[section])
		if !ok {
			return false
		}
		for _, key := range required {
			value, exists := sectionFields[key]
			if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return false
			}
		}
	}
	libraries, ok := objectFields(settings["libraries"])
	if !ok {
		return false
	}
	for source, required := range map[string][]string{
		"jellyfin":  {"enabled", "url", "mediaTypes", "tokenEnv", "usernameEnv", "passwordEnv"},
		"plex":      {"enabled", "url", "mediaTypes", "tokenEnv", "usernameEnv", "passwordEnv"},
		"romm":      {"enabled", "url", "mediaTypes", "tokenEnv", "usernameEnv", "passwordEnv"},
		"navidrome": {"enabled", "url", "mediaTypes", "tokenEnv", "usernameEnv", "passwordEnv"},
	} {
		fields, ok := objectFields(libraries[source])
		if !ok {
			return false
		}
		for _, key := range required {
			value, exists := fields[key]
			if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return false
			}
		}
	}
	return true
}

func objectFields(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, false
	}
	return fields, true
}
