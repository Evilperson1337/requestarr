package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

// writeResolveError answers a failed link resolution. The response says what
// the person can do about it (wait, ask an administrator, check the link); the
// provider's own error stays in the log. fallbackCode names the generic case.
func writeResolveError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error, fallbackCode string) {
	logger.Warn("metadata resolution failed", "request_id", apierror.RequestID(r.Context()), "error", err)
	switch {
	case errors.Is(err, metadata.ErrBusy):
		w.Header().Set("Retry-After", "5")
		apierror.Write(w, r, http.StatusServiceUnavailable, "metadata_provider_busy", "The metadata service is busy right now. Try again in a moment.")
	case errors.Is(err, metadata.ErrCredentials):
		apierror.Write(w, r, http.StatusBadGateway, "metadata_provider_credentials", "The metadata service rejected Requestarr's credentials. Ask an administrator to check them in Settings.")
	case errors.Is(err, metadata.ErrNotConfigured):
		apierror.Write(w, r, http.StatusServiceUnavailable, "metadata_provider_not_configured", "This kind of link is not set up yet. Ask an administrator to configure it in Settings.")
	default:
		apierror.Write(w, r, http.StatusBadRequest, fallbackCode, "metadata URL could not be resolved")
	}
}
