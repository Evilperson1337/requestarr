package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/checklist"
)

// ChecklistService computes the administrator's Get started list and remembers
// that it was dismissed (D038).
type ChecklistService interface {
	Get(context.Context) (checklist.Result, error)
	Dismiss(context.Context) error
}

// IGDBSettings is the non-secret IGDB client ID. An environment value wins and
// cannot be changed here.
type IGDBSettings interface {
	IGDBClientID(context.Context) (id string, fromEnvironment bool, err error)
	SetIGDBClientID(ctx context.Context, id string) error
}

var (
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

func registerChecklistRoutes(rt *routeTable, list ChecklistService, igdb IGDBSettings) {
	rt.handle("GET /admin/checklist", accessAdmin, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		result, err := list.Get(r.Context())
		if err != nil {
			apierror.Write(w, r, 500, "checklist_failed", "could not load the Get started checklist")
			return
		}
		writeJSON(w, 200, result)
	})
	rt.handle("POST /admin/checklist/dismiss", accessAdmin, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		if err := list.Dismiss(r.Context()); err != nil {
			apierror.Write(w, r, 500, "checklist_failed", "could not dismiss the Get started checklist")
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		var input struct {
			ClientID *string `json:"clientId"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || requireJSONEOF(decoder) != nil || input.ClientID == nil {
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
			writeJSON(w, 200, igdbSettingsBody{ClientID: *input.ClientID})
		}
	})
}
