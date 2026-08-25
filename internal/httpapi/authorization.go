package httpapi

import (
	"net/http"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
)

// requireAdmin fails closed before invoking next unless the browser session is
// live and its current persisted role grants settings management.
func requireAdmin(options AuthOptions, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := readAuthSession(r, options)
		switch result.state {
		case authSessionUnavailable:
			writeAPIError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "Authentication is temporarily unavailable.")
			return
		case authSessionUnauthenticated:
			writeAPIError(w, r, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			return
		case authSessionAuthenticated:
			if !result.principal.Allows(auth.PermissionManageSettings) {
				writeAPIError(w, r, http.StatusForbidden, "forbidden", "Administrator access is required.")
				return
			}
			next.ServeHTTP(w, r)
		}
	})
}
