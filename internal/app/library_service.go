package app

import (
	"context"
	"errors"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/httpapi"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
)

type libraryStore interface {
	RecentlyAdded(context.Context, int) ([]domain.RecentLibraryItem, error)
	LibraryArtworkRef(context.Context, string) (string, string, error)
}

// libraryService serves what libraries recently gained and their covers. A cover
// is fetched by the adapter that owns the item, so no credential leaves it.
type libraryService struct {
	store   libraryStore
	manager *sourceManager
}

var _ httpapi.LibraryReader = (*libraryService)(nil)

func (s *libraryService) RecentlyAdded(ctx context.Context, limit int) ([]domain.RecentLibraryItem, error) {
	return s.store.RecentlyAdded(ctx, limit)
}

func (s *libraryService) Artwork(ctx context.Context, itemID string) (inventory.Artwork, error) {
	sourceID, ref, err := s.store.LibraryArtworkRef(ctx, itemID)
	if errors.Is(err, postgres.ErrNoArtwork) {
		return inventory.Artwork{}, httpapi.ErrNoArtwork
	}
	if err != nil {
		return inventory.Artwork{}, err
	}
	source := s.manager.artworkSource(sourceID)
	if source == nil {
		return inventory.Artwork{}, httpapi.ErrNoArtwork
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return source.Artwork(ctx, ref)
}
