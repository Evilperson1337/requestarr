package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
)

type matchReviewStore interface {
	ListMatchSuggestions(context.Context) ([]domain.MatchSuggestion, error)
	ReviewMatchSuggestion(context.Context, string, string, string) error
}

func registerMatchReviewRoutes(api *http.ServeMux, readiness ReadinessChecker, authHandler *auth.Handler) {
	store, available := readiness.(matchReviewStore)
	admin := func(next func(http.ResponseWriter, *http.Request, auth.User)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user, err := authHandler.CurrentSession(r)
			if err != nil {
				apierror.Write(w, r, http.StatusUnauthorized, "authentication_required", "authentication required")
				return
			}
			if user.Role != "admin" {
				apierror.Write(w, r, http.StatusForbidden, "forbidden", "administrator role required")
				return
			}
			if !available {
				apierror.Write(w, r, http.StatusServiceUnavailable, "match_review_unavailable", "match review unavailable")
				return
			}
			next(w, r, user)
		}
	}
	api.HandleFunc("GET /admin/match-suggestions", admin(func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		items, err := store.ListMatchSuggestions(r.Context())
		if err != nil {
			apierror.Write(w, r, http.StatusInternalServerError, "match_suggestions_failed", "could not load match suggestions")
			return
		}
		if items == nil {
			items = []domain.MatchSuggestion{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}))
	for _, decision := range []string{"accept", "dismiss"} {
		api.HandleFunc("POST /admin/match-suggestions/{suggestionID}/"+decision, admin(func(w http.ResponseWriter, r *http.Request, user auth.User) {
			id := r.PathValue("suggestionID")
			if !validUUID(id) {
				apierror.Write(w, r, http.StatusBadRequest, "invalid_suggestion_id", "invalid suggestion ID")
				return
			}
			err := store.ReviewMatchSuggestion(r.Context(), id, user.ID, decision)
			switch {
			case errors.Is(err, domain.ErrMatchNotFound):
				apierror.Write(w, r, http.StatusNotFound, "suggestion_not_found", "match suggestion not found")
			case errors.Is(err, domain.ErrMatchAlreadyReviewed):
				apierror.Write(w, r, http.StatusConflict, "suggestion_already_reviewed", "this suggestion has already been reviewed; refresh the list")
			case errors.Is(err, domain.ErrMatchStale):
				apierror.Write(w, r, http.StatusConflict, "suggestion_stale", "this suggestion is no longer current; refresh the list after a successful source sync")
			case err != nil:
				apierror.Write(w, r, http.StatusInternalServerError, "match_review_failed", "could not review match suggestion")
			default:
				w.WriteHeader(http.StatusNoContent)
			}
		}))
	}
}
