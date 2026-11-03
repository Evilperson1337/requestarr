package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/sources"
)

type SetupService interface {
	Load(context.Context, setup.Actor) (setup.Draft, error)
	Save(context.Context, setup.Actor, setup.SaveInput) (setup.Draft, error)
	Validate(context.Context, setup.Actor, int64, setup.Target) (setup.Draft, error)
	Finish(context.Context, setup.Actor, int64) (setup.Draft, error)
}

func registerSetupRoutes(rt *routeTable, service SetupService) {
	admin := func(pattern string, next func(http.ResponseWriter, *http.Request, setup.Actor)) {
		rt.handle(pattern, accessAdmin, func(w http.ResponseWriter, r *http.Request, user auth.User) {
			w.Header().Set("Cache-Control", "no-store")
			if service == nil {
				apierror.Write(w, r, 503, "setup_unavailable", "setup draft unavailable")
				return
			}
			next(w, r, setup.Actor{ID: user.ID, Role: user.Role})
		})
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
	admin("GET /admin/setup/source-kinds", func(w http.ResponseWriter, r *http.Request, _ setup.Actor) {
		writeJSON(w, 200, sourceKindsBody())
	})
	admin("GET /admin/setup/draft", func(w http.ResponseWriter, r *http.Request, actor setup.Actor) {
		draft, err := service.Load(r.Context(), actor)
		respond(w, r, draft, err)
	})
	admin("PUT /admin/setup/draft", func(w http.ResponseWriter, r *http.Request, actor setup.Actor) {
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
	})
	admin("POST /admin/setup/validate", func(w http.ResponseWriter, r *http.Request, actor setup.Actor) {
		var input struct {
			ExpectedRevision int64        `json:"expectedRevision"`
			Target           setup.Target `json:"target"`
		}
		if !decode(w, r, &input) {
			return
		}
		draft, err := service.Validate(r.Context(), actor, input.ExpectedRevision, input.Target)
		respond(w, r, draft, err)
	})
	admin("POST /admin/setup/finish", func(w http.ResponseWriter, r *http.Request, actor setup.Actor) {
		var input struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if !decode(w, r, &input) {
			return
		}
		draft, err := service.Finish(r.Context(), actor, input.ExpectedRevision)
		respond(w, r, draft, err)
	})
}

// hasRequiredDraftFields reports whether a save carries every field of the
// draft schema, none null, so a partial body can never blank a setting. The
// required keys are read from the Settings type, which also fixes the Source
// kind keys (see setup.LibrariesSettings), instead of being listed here.
func hasRequiredDraftFields(body []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return false
	}
	return hasEveryField(fields["settings"], reflect.TypeOf(setup.Settings{}))
}

// hasEveryField checks raw is an object that has every JSON-tagged field of
// the struct type t, non-null, recursing into struct fields.
func hasEveryField(raw json.RawMessage, t reflect.Type) bool {
	fields, ok := objectFields(raw)
	if !ok {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		value, exists := fields[name]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
		if field.Type.Kind() == reflect.Struct && !hasEveryField(value, field.Type) {
			return false
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

type sourceKindBody struct {
	Slug        string                 `json:"slug"`
	Name        string                 `json:"name"`
	URLEnv      string                 `json:"urlEnv"`
	MediaTypes  []string               `json:"mediaTypes"`
	Credentials []sourceCredentialBody `json:"credentials"`
}

type sourceCredentialBody struct {
	Role   string `json:"role"`
	Env    string `json:"env"`
	Secret string `json:"secret,omitempty"`
	// Label names the secret field in the UI; empty for a plain field.
	Label string `json:"label,omitempty"`
}

// sourceKindsBody is the Source kind registry as the wizard renders it.
func sourceKindsBody() map[string]any {
	kinds := []sourceKindBody{}
	for _, kind := range sources.All() {
		body := sourceKindBody{Slug: kind.Slug, Name: kind.Name, URLEnv: kind.URLEnv, MediaTypes: []string{}, Credentials: []sourceCredentialBody{}}
		for _, media := range kind.MediaTypes {
			body.MediaTypes = append(body.MediaTypes, string(media))
		}
		for _, credential := range kind.Credentials {
			item := sourceCredentialBody{Role: string(credential.Role), Env: credential.Env, Secret: credential.Secret}
			if spec, ok := secrets.Lookup(secrets.Name(credential.Secret)); ok {
				item.Label = spec.Label
			}
			body.Credentials = append(body.Credentials, item)
		}
		kinds = append(kinds, body)
	}
	return map[string]any{"kinds": kinds}
}
