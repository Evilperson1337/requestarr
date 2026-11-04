package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

// SearchService finds items by name at the provider for one search type. It
// never writes.
type SearchService interface {
	// Types lists the search types whose provider is configured.
	Types() []string
	Search(ctx context.Context, searchType, text string, page int, opts metadata.SearchOptions) (domain.SearchPage, error)
}

const (
	searchMinChars = 2
	searchMaxChars = 200
	searchMaxPage  = 10
)

// searchTypes are every type the API names, configured or not.
var searchTypes = map[string]bool{"movie_tv": true, "album": true, "song": true, "game": true, "audiobook": true}

func registerSearchRoutes(rt *routeTable, search SearchService, logger *slog.Logger) {
	rt.handle("GET /search/types", accessSession, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string][]string{"types": search.Types()})
	})
	rt.handle("GET /search", accessSession, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		q := r.URL.Query()
		text := strings.TrimSpace(q.Get("q"))
		if n := utf8.RuneCountInString(text); n < searchMinChars || n > searchMaxChars || !utf8.ValidString(text) ||
			strings.IndexFunc(text, unicode.IsControl) >= 0 {
			apierror.Write(w, r, http.StatusBadRequest, "invalid_search", "Search text must be 2 to 200 characters without control characters.")
			return
		}
		searchType := q.Get("type")
		if !searchTypes[searchType] {
			apierror.Write(w, r, http.StatusBadRequest, "invalid_search", "type must be movie_tv, album, song, game or audiobook.")
			return
		}
		page := 1
		if raw := q.Get("page"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > searchMaxPage {
				apierror.Write(w, r, http.StatusBadRequest, "invalid_search", "page must be 1 to 10.")
				return
			}
			page = n
		}
		result, err := search.Search(r.Context(), searchType, text, page, metadata.SearchOptions{IncludeSingles: q.Get("singles") == "true"})
		if err != nil {
			writeSearchError(w, r, logger, err)
			return
		}
		if result.Results == nil {
			result.Results = []domain.SearchResult{}
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
	})
}

// writeSearchError answers a failed search with the resolve error codes. A
// provider failure is never an empty list: whatever went wrong, the person is
// told the search did not work. The search text is not logged.
func writeSearchError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	logger.Warn("metadata search failed", "request_id", apierror.RequestID(r.Context()), "error", err)
	switch {
	case errors.Is(err, metadata.ErrCredentials):
		apierror.Write(w, r, http.StatusBadGateway, "metadata_provider_credentials", "The metadata service rejected Requestarr's credentials. Ask an administrator to check them in Settings.")
	case errors.Is(err, metadata.ErrNotConfigured):
		apierror.Write(w, r, http.StatusServiceUnavailable, "metadata_provider_not_configured", "This kind of search is not set up yet. Ask an administrator to configure it in Settings.")
	default:
		// Busy, down, or an answer Requestarr could not read: try again later.
		w.Header().Set("Retry-After", "5")
		apierror.Write(w, r, http.StatusServiceUnavailable, "metadata_provider_busy", "The metadata service is busy right now. Try again in a moment.")
	}
}
