//go:build integration

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/inventory/plex"
	"github.com/evilperson1337/requestarr/internal/requests"
)

func TestRecentlyAddedOrdersByAddedDateSkipsSongsAndRemovedItemsIntegration(t *testing.T) {
	ctx, s, _ := setupDraftStore(t)
	d := inventory.Descriptor{ID: plex.SourceID, Kind: "plex", Name: "Plex", MediaTypes: []domain.MediaType{domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaSong}}
	item := func(id, title string, media domain.MediaType, added time.Time, ref string) inventory.Item {
		return inventory.Item{SourceID: d.ID, SourceItemID: id, MediaType: media, Title: title, AddedAt: added, ArtworkRef: ref}
	}
	old, newer := time.Now().Add(-48*time.Hour).UTC().Truncate(time.Second), time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	first := inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().Add(-time.Minute).UTC(), Items: []inventory.Item{
		item("r1", "Older movie", domain.MediaMovie, old, "/library/metadata/1/thumb/1"),
		item("r2", "Newer album", domain.MediaAlbum, newer, ""),
		item("r3", "A song", domain.MediaSong, time.Now().UTC(), ""),
		item("r4", "Removed later", domain.MediaTV, newer.Add(time.Minute), ""),
	}}
	if err := s.PersistSnapshot(ctx, d, "http://plex.example.test", first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ObservedAt = time.Now().UTC()
	second.Items = first.Items[:3] // "Removed later" is gone from the library
	if err := s.PersistSnapshot(ctx, d, "http://plex.example.test", second); err != nil {
		t.Fatal(err)
	}
	got, err := s.RecentlyAdded(ctx, 50, []domain.MediaType{domain.MediaMovie, domain.MediaAlbum})
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, it := range got {
		if it.Source == "Plex" {
			titles = append(titles, it.Title)
		}
	}
	if len(titles) < 2 || titles[0] != "Newer album" || titles[1] != "Older movie" {
		t.Fatalf("titles = %v", titles)
	}
	for _, title := range titles {
		if title == "A song" || title == "Removed later" {
			t.Fatalf("%q must not be listed", title)
		}
	}
	for _, it := range got {
		if it.Title == "Older movie" && !strings.HasPrefix(it.ArtworkURL, "/api/v1/library/items/") {
			t.Fatalf("cover link missing: %+v", it)
		}
		if it.Title == "Newer album" && it.ArtworkURL != "" {
			t.Fatal("an item with no cover must not advertise one")
		}
		if it.Title == "Older movie" {
			id := strings.TrimSuffix(strings.TrimPrefix(it.ArtworkURL, "/api/v1/library/items/"), "/artwork")
			src, ref, err := s.LibraryArtworkRef(context.Background(), id)
			if err != nil || src != d.ID || ref != "/library/metadata/1/thumb/1" {
				t.Fatalf("ref %q %q %v", src, ref, err)
			}
		}
	}
	if _, _, err := s.LibraryArtworkRef(ctx, "not-a-uuid"); err != ErrNoArtwork {
		t.Fatalf("bad id err = %v", err)
	}
}

// A title resolved after its library synced is available at once: the catalog
// item is matched against the inventory already stored.
func TestResolvingAfterTheSyncMatchesAgainstStoredInventoryIntegration(t *testing.T) {
	ctx, s, user := setupDraftStore(t)
	d := inventory.Descriptor{ID: plex.SourceID, Kind: "plex", Name: "Plex", MediaTypes: []domain.MediaType{domain.MediaMovie}, IdentifierKinds: []string{"tmdb"}}
	tmdbID := newUUID()[:8]
	snap := inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().UTC(), Items: []inventory.Item{
		{SourceID: d.ID, SourceItemID: "late-" + tmdbID, MediaType: domain.MediaMovie, Title: "Late arrival", ExternalIDs: map[string]string{"tmdb": tmdbID}},
	}}
	if err := s.PersistSnapshot(ctx, d, "http://plex.example.test", snap); err != nil {
		t.Fatal(err)
	}
	item, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaMovie, Provider: "tmdb", ProviderID: tmdbID, Title: "Late arrival"}})
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.MoviePreview(ctx, item.ID, user)
	if err != nil || state.Availability != domain.AvailabilityAvailable || len(state.Libraries) != 1 || state.Libraries[0].Name != "Plex" {
		t.Fatalf("preview = %+v err %v", state, err)
	}
	if _, err := requests.New(s).Create(ctx, requests.CreateInput{Actor: requests.Actor{ID: user, Role: "requester"}, CatalogID: item.ID}); !errors.Is(err, requests.ErrAlreadyAvailable) {
		t.Fatalf("requesting what a library already has: err = %v", err)
	}
	other, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaMovie, Provider: "tmdb", ProviderID: tmdbID + "9", Title: "Not in Plex"}})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := s.MoviePreview(ctx, other.ID, user); st.Availability == domain.AvailabilityAvailable {
		t.Fatal("a title no library holds must not become available")
	}
}
