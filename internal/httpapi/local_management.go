package httpapi

import (
	"context"
	"errors"
	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"net/http"
)

type LocalAdminManagement interface {
	LocalAdminStatus(context.Context) (auth.LocalAdminStatus, error)
	Create(context.Context, string) (auth.CreatedLocalAdmin, error)
	SetDisabled(context.Context, string, bool) error
}

func registerLocalManagementRoutes(rt *routeTable, s LocalAdminManagement) {
	fail := func(w http.ResponseWriter, r *http.Request, err error) {
		switch {
		case errors.Is(err, auth.ErrLocalAdminExists):
			apierror.Write(w, r, 409, "local_admin_exists", auth.ErrLocalAdminExists.Error())
		case errors.Is(err, auth.ErrLastLocalAdmin):
			apierror.Write(w, r, 409, "last_admin", auth.ErrLastLocalAdmin.Error())
		case errors.Is(err, auth.ErrNoWorkingSignIn):
			apierror.Write(w, r, 409, "no_working_sign_in", auth.ErrNoWorkingSignIn.Error())
		case errors.Is(err, auth.ErrNoCredential):
			apierror.Write(w, r, 404, "local_admin_missing", "the local administrator does not exist")
		default:
			apierror.Write(w, r, 500, "local_admin_update_failed", "could not update the local administrator")
		}
	}
	status := func(w http.ResponseWriter, r *http.Request) {
		st, err := s.LocalAdminStatus(r.Context())
		if err != nil {
			fail(w, r, err)
			return
		}
		writeJSON(w, 200, st)
	}
	rt.handle("GET /admin/settings/authentication/local-administrator", accessAdmin, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		status(w, r)
	})
	rt.handle("POST /admin/settings/authentication/local-administrator", accessAdmin, func(w http.ResponseWriter, r *http.Request, u auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		credential, err := s.Create(r.Context(), u.ID)
		if err != nil {
			fail(w, r, err)
			return
		}
		writeJSON(w, 201, credential)
	})
	rt.handle("PUT /admin/settings/authentication/local-administrator/disabled", accessAdmin, func(w http.ResponseWriter, r *http.Request, u auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		var in struct {
			Disabled *bool `json:"disabled"`
		}
		if !decodeSettingsBody(w, r, &in) {
			return
		}
		if in.Disabled == nil {
			apierror.Write(w, r, 422, "invalid_disabled", "disabled is required")
			return
		}
		if err := s.SetDisabled(r.Context(), u.ID, *in.Disabled); err != nil {
			fail(w, r, err)
			return
		}
		status(w, r)
	})
}
