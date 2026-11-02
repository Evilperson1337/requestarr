//go:build integration

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/inventory/plex"
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
	got, err := s.RecentlyAdded(ctx, 10)
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
