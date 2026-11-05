package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
)

// PeoplePolicy holds the instance-wide rules for people (D035). Only viewer and
// requester are valid first-sign-in roles; the implementation rejects anything
// else with ErrInvalidDefaultRole.
type PeoplePolicy interface {
	DefaultFirstSignInRole(context.Context) (domain.Role, error)
	SetDefaultFirstSignInRole(context.Context, domain.Role) error
}

// ApprovalPolicy holds the media types whose new Requests are approved
// automatically (D036). The implementation rejects anything that is not a
// media type with ErrInvalidMediaType.
type ApprovalPolicy interface {
	AutoApproveTypes(context.Context) ([]domain.MediaType, error)
	SetAutoApproveTypes(ctx context.Context, types []domain.MediaType) error
}

// IGDBSettings is the non-secret IGDB client ID. An environment value wins and
// cannot be changed here.
type IGDBSettings interface {
	IGDBClientID(context.Context) (id string, fromEnvironment bool, err error)
	SetIGDBClientID(ctx context.Context, id string) error
}

// Errors the settings implementations return; the app layer maps its store's
// errors onto these so this file never names an adapter.
var (
	ErrInvalidDefaultRole          = errors.New("default role must be viewer or requester")
	ErrInvalidMediaType            = errors.New("unknown media type")
	ErrInvalidIGDBClientID         = errors.New("invalid IGDB client ID")
	ErrIGDBClientIDFromEnvironment = errors.New("IGDB client ID is set by the environment")
	// ErrNotApplied means a credential was saved but the running providers could
	// not be rebuilt from it; the previous configuration is still in use (D039).
	ErrNotApplied = errors.New("saved but not applied")
)

type igdbSettingsBody struct {
	ClientID        string `json:"clientId"`
	FromEnvironment bool   `json:"fromEnvironment"`
}

// decodeSettingsBody reads exactly one small JSON object with no unknown
// fields into dst. On failure it writes 400 invalid_json and returns false.
func decodeSettingsBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil || requireJSONEOF(decoder) != nil {
		apierror.Write(w, r, 400, "invalid_json", "provide one JSON object with only the documented fields")
		return false
	}
	return true
}

// nonNilTypes makes an empty policy encode as [] rather than null.
func nonNilTypes(types []domain.MediaType) []domain.MediaType {
	if types == nil {
		return []domain.MediaType{}
	}
	return types
}

// registerSettingsRoutes serves the administrator's people, approval and IGDB
// client ID settings. Every PUT answers with the value as stored.
func registerSettingsRoutes(rt *routeTable, people PeoplePolicy, approvals ApprovalPolicy, igdb IGDBSettings) {
	rt.handle("GET /admin/settings/people", accessAdmin, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		role, err := people.DefaultFirstSignInRole(r.Context())
		if err != nil {
			apierror.Write(w, r, 500, "people_settings_failed", "could not load the people settings")
			return
		}
		writeJSON(w, 200, map[string]domain.Role{"defaultRole": role})
	})
	rt.handle("PUT /admin/settings/people", accessAdmin, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		var input struct {
			DefaultRole *domain.Role `json:"defaultRole"`
		}
		if !decodeSettingsBody(w, r, &input) {
			return
		}
		if input.DefaultRole == nil {
			apierror.Write(w, r, 400, "invalid_default_role", "defaultRole must be viewer or requester")
			return
		}
		switch err := people.SetDefaultFirstSignInRole(r.Context(), *input.DefaultRole); {
		case errors.Is(err, ErrInvalidDefaultRole):
			apierror.Write(w, r, 400, "invalid_default_role", "defaultRole must be viewer or requester")
		case err != nil:
			apierror.Write(w, r, 500, "people_settings_failed", "could not save the people settings")
		default:
			saved, err := people.DefaultFirstSignInRole(r.Context())
			if err != nil {
				apierror.Write(w, r, 500, "people_settings_failed", "could not load the people settings")
				return
			}
			writeJSON(w, 200, map[string]domain.Role{"defaultRole": saved})
		}
	})
	rt.handle("GET /admin/settings/approvals", accessAdmin, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		types, err := approvals.AutoApproveTypes(r.Context())
		if err != nil {
			apierror.Write(w, r, 500, "approval_settings_failed", "could not load the approval settings")
			return
		}
		writeJSON(w, 200, map[string][]domain.MediaType{"autoApproveTypes": nonNilTypes(types)})
	})
	rt.handle("PUT /admin/settings/approvals", accessAdmin, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		var input struct {
			AutoApproveTypes *[]domain.MediaType `json:"autoApproveTypes"`
		}
		if !decodeSettingsBody(w, r, &input) {
			return
		}
		if input.AutoApproveTypes == nil {
			apierror.Write(w, r, 400, "invalid_media_type", "autoApproveTypes must be a list of media types")
			return
		}
		switch err := approvals.SetAutoApproveTypes(r.Context(), *input.AutoApproveTypes); {
		case errors.Is(err, ErrInvalidMediaType):
			apierror.Write(w, r, 400, "invalid_media_type", "autoApproveTypes must be a list of media types")
		case err != nil:
			apierror.Write(w, r, 500, "approval_settings_failed", "could not save the approval settings")
		default:
			saved, err := approvals.AutoApproveTypes(r.Context())
			if err != nil {
				apierror.Write(w, r, 500, "approval_settings_failed", "could not load the approval settings")
				return
			}
			writeJSON(w, 200, map[string][]domain.MediaType{"autoApproveTypes": nonNilTypes(saved)})
		}
	})
	rt.handle("GET /admin/settings/igdb", accessAdmin, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		id, fromEnv, err := igdb.IGDBClientID(r.Context())
		if err != nil {
			apierror.Write(w, r, 500, "igdb_settings_failed", "could not load the IGDB client ID")
			return
		}
		writeJSON(w, 200, igdbSettingsBody{ClientID: id, FromEnvironment: fromEnv})
	})
	rt.handle("PUT /admin/settings/igdb", accessAdmin, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		var input struct {
			ClientID *string `json:"clientId"`
		}
		if !decodeSettingsBody(w, r, &input) {
			return
		}
		if input.ClientID == nil {
			apierror.Write(w, r, 400, "invalid_client_id", "provide one JSON object with clientId")
			return
		}
		switch err := igdb.SetIGDBClientID(r.Context(), *input.ClientID); {
		case errors.Is(err, ErrInvalidIGDBClientID):
			apierror.Write(w, r, 400, "invalid_client_id", "the client ID must be plain text of at most 256 characters")
		case errors.Is(err, ErrIGDBClientIDFromEnvironment):
			apierror.Write(w, r, 409, "managed_by_environment", "the client ID is set by REQUESTARR_IGDB_CLIENT_ID; change it there")
		case errors.Is(err, ErrNotApplied):
			apierror.Write(w, r, 500, "settings_not_applied", "the client ID was saved but could not be applied; the previous configuration is still in use")
		case err != nil:
			apierror.Write(w, r, 500, "igdb_settings_failed", "could not save the IGDB client ID")
		default:
			id, fromEnv, err := igdb.IGDBClientID(r.Context())
			if err != nil {
				apierror.Write(w, r, 500, "igdb_settings_failed", "could not load the IGDB client ID")
				return
			}
			writeJSON(w, 200, igdbSettingsBody{ClientID: id, FromEnvironment: fromEnv})
		}
	})
}
