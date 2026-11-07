package httpapi

import (
	"context"
	"errors"
	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"net/http"
)

// AuthenticationService is the Authentication settings page's view of the OIDC
// state: read it, save, test or discard the saved settings, and switch OIDC on
// or off. The string argument is the acting administrator, for the audit trail.
type AuthenticationService interface {
	Status(context.Context) (authsettings.Status, error)
	Options() authsettings.Options
	Save(context.Context, string, authsettings.Update) error
	Test(context.Context, authsettings.Update) (warnings []string, err error)
	SetEnabled(context.Context, string, bool) error
	Discard(context.Context, string) error
}

func registerAuthenticationRoutes(rt *routeTable, s AuthenticationService) {
	rt.handle("GET /auth/options", accessPublic, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, s.Options())
	})
	fail := func(w http.ResponseWriter, r *http.Request, err error) {
		status, code, message := 500, "authentication_settings_failed", "could not update authentication settings; previous configuration remains active"
		switch {
		case errors.Is(err, authsettings.ErrReadOnly):
			status, code, message = 409, "oidc_environment_override", authsettings.ErrReadOnly.Error()
		case errors.Is(err, secrets.ErrNoMasterKey):
			status, code, message = 409, "secrets_disabled", "REQUESTARR_SECRETS_KEY is not configured, so secret values cannot be saved"
		case errors.Is(err, secrets.ErrUnreadable):
			status, code, message = 422, "oidc_secret_unreadable", "restore REQUESTARR_SECRETS_KEY or re-enter the client secret"
		case errors.Is(err, authsettings.ErrNoOtherSignIn):
			status, code, message = 409, "oidc_is_only_sign_in", authsettings.ErrNoOtherSignIn.Error()
		case errors.Is(err, authsettings.ErrInvalid):
			status, code, message = 422, "invalid_oidc_settings", authsettings.ErrInvalid.Error()
		case errors.Is(err, authsettings.ErrDiscovery):
			status, code, message = 422, "oidc_discovery_failed", authsettings.DiscoveryMessage(err)
		}
		apierror.Write(w, r, status, code, message)
	}
	respond := func(w http.ResponseWriter, r *http.Request) {
		st, err := s.Status(r.Context())
		if err != nil {
			fail(w, r, err)
			return
		}
		writeJSON(w, 200, st)
	}
	admin := func(pattern string, f func(http.ResponseWriter, *http.Request, string)) {
		rt.handle(pattern, accessAdmin, func(w http.ResponseWriter, r *http.Request, u auth.User) {
			w.Header().Set("Cache-Control", "no-store")
			f(w, r, u.ID)
		})
	}
	admin("GET /admin/settings/authentication", func(w http.ResponseWriter, r *http.Request, _ string) { respond(w, r) })
	admin("PUT /admin/settings/authentication", func(w http.ResponseWriter, r *http.Request, actor string) {
		var u authsettings.Update
		if !decodeSettingsBody(w, r, &u, authenticationBodyLimit) {
			return
		}
		if err := s.Save(r.Context(), actor, u); err != nil {
			fail(w, r, err)
			return
		}
		respond(w, r)
	})
	admin("POST /admin/settings/authentication/test", func(w http.ResponseWriter, r *http.Request, _ string) {
		var u authsettings.Update
		if !decodeSettingsBody(w, r, &u, authenticationBodyLimit) {
			return
		}
		warnings, err := s.Test(r.Context(), u)
		if err != nil {
			fail(w, r, err)
			return
		}
		if warnings == nil {
			warnings = []string{}
		}
		writeJSON(w, 200, map[string]any{"ok": true, "warnings": warnings})
	})
	admin("PUT /admin/settings/authentication/enabled", func(w http.ResponseWriter, r *http.Request, actor string) {
		var u struct {
			Enabled *bool `json:"enabled"`
		}
		if !decodeSettingsBody(w, r, &u, authenticationBodyLimit) {
			return
		}
		if u.Enabled == nil {
			apierror.Write(w, r, 422, "invalid_oidc_settings", "enabled is required")
			return
		}
		if err := s.SetEnabled(r.Context(), actor, *u.Enabled); err != nil {
			fail(w, r, err)
			return
		}
		respond(w, r)
	})
	admin("DELETE /admin/settings/authentication", func(w http.ResponseWriter, r *http.Request, actor string) {
		if err := s.Discard(r.Context(), actor); err != nil {
			fail(w, r, err)
			return
		}
		respond(w, r)
	})
}
