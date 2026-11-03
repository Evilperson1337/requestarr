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
	RecentlyAdded(context.Context, int, []domain.MediaType) ([]domain.RecentLibraryItem, error)
	LibraryArtworkRef(context.Context, string) (string, string, error)
	BrowseLibrary(context.Context, domain.LibraryQuery) (domain.LibraryPage, error)
	LibraryItemByID(context.Context, string) (domain.LibraryItem, error)
}

// libraryService serves what libraries recently gained and their covers. A cover
// is fetched by the adapter that owns the item, so no credential leaves it.
type libraryService struct {
	store   libraryStore
	manager *sourceManager
}

var _ httpapi.LibraryReader = (*libraryService)(nil)

func (s *libraryService) RecentlyAdded(ctx context.Context, limit int, types []domain.MediaType) ([]domain.RecentLibraryItem, error) {
	return s.store.RecentlyAdded(ctx, limit, types)
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

func (s *libraryService) Browse(ctx context.Context, q domain.LibraryQuery) (domain.LibraryPage, error) {
	page, err := s.store.BrowseLibrary(ctx, q)
	if errors.Is(err, postgres.ErrBadLibraryCursor) {
		return domain.LibraryPage{}, httpapi.ErrBadLibraryCursor
	}
	return page, err
}

func (s *libraryService) Item(ctx context.Context, id string) (domain.LibraryItem, error) {
	item, err := s.store.LibraryItemByID(ctx, id)
	if errors.Is(err, postgres.ErrLibraryItemNotFound) {
		return domain.LibraryItem{}, httpapi.ErrLibraryItemNotFound
	}
	return item, err
}
