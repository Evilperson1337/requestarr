package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

// ErrNoArtwork is returned when an item has no cover to serve.
var ErrNoArtwork = errors.New("no artwork")

// LibraryReader is what the libraries recently gained, and the covers for it (D029).
type LibraryReader interface {
	RecentlyAdded(ctx context.Context, limit int) ([]domain.RecentLibraryItem, error)
	Artwork(ctx context.Context, itemID string) (inventory.Artwork, error)
}

func registerLibraryRoutes(rt *routeTable, library LibraryReader) {
	rt.handle("GET /library/recent", accessSession, func(w http.ResponseWriter, r *http.Request, _ auth.User) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, err := library.RecentlyAdded(r.Context(), limit)
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
