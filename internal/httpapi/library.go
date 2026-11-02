package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// ErrNoArtwork is returned when an item has no cover to serve.
var ErrNoArtwork = errors.New("no artwork")

// LibraryReader is what the libraries recently gained, and the covers for it (D029).
type LibraryReader interface {
	RecentlyAdded(ctx context.Context, limit int, types []domain.MediaType) ([]domain.RecentLibraryItem, error)
	Artwork(ctx context.Context, itemID string) (inventory.Artwork, error)
	Browse(ctx context.Context, q domain.LibraryQuery) (domain.LibraryPage, error)
	Item(ctx context.Context, id string) (domain.LibraryItem, error)
}

// ErrLibraryItemNotFound and ErrBadLibraryCursor are the Library page's client errors.
var (
	ErrLibraryItemNotFound = errors.New("library item not found")
	ErrBadLibraryCursor    = errors.New("invalid library cursor")
)

func registerLibraryRoutes(rt *routeTable, library LibraryReader) {
	rt.handle("GET /library/items", accessSession, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		q := r.URL.Query()
		query := domain.LibraryQuery{Type: domain.MediaType(q.Get("type")), Search: q.Get("q"), Source: q.Get("source"), After: q.Get("cursor")}
		switch query.Type {
		case domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaGame:
		default:
			apierror.Write(w, r, 400, "invalid_query", "type must be movie, tv, album or game")
			return
		}
		if _, ok := sources.Lookup(query.Source); query.Source != "" && !ok || len(query.Search) > 100 || len(query.After) > 200 {
			apierror.Write(w, r, 400, "invalid_query", "invalid source, search or cursor")
			return
		}
		if raw := q.Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 100 {
				apierror.Write(w, r, 400, "invalid_query", "limit must be 1 to 100")
				return
			}
			query.Limit = n
		}
		page, err := library.Browse(r.Context(), query)
		switch {
		case errors.Is(err, ErrBadLibraryCursor):
			apierror.Write(w, r, 400, "invalid_cursor", "the cursor is not valid")
			return
		case err != nil:
			apierror.Write(w, r, 500, "library_browse_failed", "could not load the library")
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=30")
		writeJSON(w, 200, page)
	})
	rt.handle("GET /library/items/{id}", accessSession, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		item, err := library.Item(r.Context(), r.PathValue("id"))
		switch {
		case errors.Is(err, ErrLibraryItemNotFound):
			apierror.Write(w, r, 404, "library_item_not_found", "no such library item")
			return
		case err != nil:
			apierror.Write(w, r, 500, "library_browse_failed", "could not load the library item")
			return
		}
		w.Header().Set("Cache-Control", "private, no-cache")
		writeJSON(w, 200, item)
	})
	rt.handle("GET /library/recent", accessSession, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		var types []domain.MediaType
		for _, name := range strings.Split(r.URL.Query().Get("types"), ",") {
			if name != "" {
				types = append(types, domain.MediaType(name))
			}
		}
		items, err := library.RecentlyAdded(r.Context(), limit, types)
		if err != nil {
			apierror.Write(w, r, 500, "library_recent_failed", "could not load recently added items")
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=60")
		writeJSON(w, 200, map[string]any{"items": items})
	})
	rt.handle("GET /library/items/{id}/artwork", accessSession, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		art, err := library.Artwork(r.Context(), r.PathValue("id"))
		switch {
		case errors.Is(err, ErrNoArtwork):
			apierror.Write(w, r, 404, "artwork_not_found", "no artwork for this item")
			return
		case err != nil:
			// Library errors name addresses; never echo them.
			apierror.Write(w, r, 502, "artwork_unavailable", "the library could not supply this cover")
			return
		}
		w.Header().Set("Content-Type", art.ContentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		_, _ = w.Write(art.Data)
	})
}
