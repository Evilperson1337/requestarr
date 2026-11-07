package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
)

type MoviePreviewReader interface {
	MoviePreview(context.Context, string, string) (domain.MoviePreview, error)
}

func moviePreview(ctx context.Context, reader MoviePreviewReader, catalogID string, user auth.User) (domain.MoviePreview, error) {
	state, err := reader.MoviePreview(ctx, catalogID, user.ID)
	if err != nil {
		return state, err
	}
	state.CanRequest = domain.Role(user.Role).CanRequest() && state.Request == nil && state.Availability != domain.AvailabilityAvailable
	state.CanExtend = domain.Role(user.Role).CanRequest() && state.Request != nil && state.Request.Status.IsOpen() && !state.Request.Selection.IsAll()
	state.CanSubscribe = state.Request != nil && state.Request.Status.IsOpen() && !state.Request.Subscribed
	return state, nil
}

func writeMoviePreviewError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, requests.ErrCatalogNotFound) {
		apierror.Write(w, r, 404, "catalog_not_found", "catalog item was not found")
		return
	}
	apierror.Write(w, r, 503, "preview_state_unavailable", "request and availability state could not be loaded; retry")
}
