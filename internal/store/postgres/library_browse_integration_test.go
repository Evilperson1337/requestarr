//go:build integration

package postgres

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

func TestBrowseLibraryFiltersPagesAndHidesWhatLeftIntegration(t *testing.T) {
	ctx, s, _ := setupDraftStore(t)
	d := inventory.Descriptor{ID: newUUID(), Kind: "romm", Name: "Browse fixture", MediaTypes: []domain.MediaType{domain.MediaGame, domain.MediaMovie}}
	base := time.Now().Add(-10 * time.Hour).UTC().Truncate(time.Second)
	var items []inventory.Item
	for i := 0; i < 5; i++ {
		items = append(items, inventory.Item{SourceID: d.ID, SourceItemID: fmt.Sprintf("g%d", i), MediaType: domain.MediaGame, Title: fmt.Sprintf("bfx Quest %d", i), AddedAt: base.Add(time.Duration(i) * time.Hour), Detail: "Windows"})
	}
	items = append(items,
		// The same game on two platforms in one Source is one entry.
		inventory.Item{SourceID: d.ID, SourceItemID: "dup1", MediaType: domain.MediaGame, Title: "bfx Twin", Year: 2020, AddedAt: base},
		inventory.Item{SourceID: d.ID, SourceItemID: "dup2", MediaType: domain.MediaGame, Title: "bfx Twin", Year: 2020, AddedAt: base.Add(30 * time.Minute)},
		inventory.Item{SourceID: d.ID, SourceItemID: "m1", MediaType: domain.MediaMovie, Title: "bfx Not a game", AddedAt: base},
		inventory.Item{SourceID: d.ID, SourceItemID: "100%", MediaType: domain.MediaGame, Title: "bfx 100% Orange Juice", AddedAt: base},
	)
	if err := s.PersistSnapshot(ctx, d, "http://romm.example.test", inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().UTC(), Items: items}); err != nil {
		t.Fatal(err)
	}
	q := domain.LibraryQuery{Type: domain.MediaGame, Search: "bfx", Limit: 3}
	first, err := s.BrowseLibrary(ctx, q)
	if err != nil || len(first.Items) != 3 || first.NextCursor == nil {
		t.Fatalf("page 1: %+v err %v", first, err)
	}
	if first.Items[0].Title != "bfx Quest 4" || first.Items[0].Detail != "Windows" || first.Items[0].SourceKind != "romm" {
		t.Fatalf("newest first: %+v", first.Items[0])
	}
	var titles []string
	seen := map[string]bool{}
	for page, cursor := first, first.NextCursor; ; {
		for _, it := range page.Items {
			if seen[it.ID] {
				t.Fatalf("an item repeated across pages: %s", it.Title)
			}
			seen[it.ID] = true
			titles = append(titles, it.Title)
		}
		if cursor == nil {
			break
		}
		q.After = *cursor
		if page, err = s.BrowseLibrary(ctx, q); err != nil {
			t.Fatal(err)
		}
		cursor = page.NextCursor
	}
	count := map[string]int{}
	for _, title := range titles {
		count[title]++
	}
	if count["bfx Twin"] != 1 || count["bfx Not a game"] != 0 || len(titles) != 7 {
		t.Fatalf("titles = %v", titles)
	}
	// Search is a literal, case-insensitive substring, so % and _ are not wildcards.
	if got, _ := s.BrowseLibrary(ctx, domain.LibraryQuery{Type: domain.MediaGame, Search: "% orange"}); len(got.Items) != 1 || got.Items[0].Title != "bfx 100% Orange Juice" {
		t.Fatalf("search = %+v", got.Items)
	}
	if got, _ := s.BrowseLibrary(ctx, domain.LibraryQuery{Type: domain.MediaGame, Search: "bfx _uest"}); len(got.Items) != 0 {
		t.Fatalf("an underscore must match only a literal underscore: %d", len(got.Items))
	}
	if _, err := s.BrowseLibrary(ctx, domain.LibraryQuery{Type: domain.MediaGame, After: "garbage"}); !errors.Is(err, ErrBadLibraryCursor) {
		t.Fatalf("bad cursor err = %v", err)
	}
	// An item the Source no longer reports disappears from the list but can still be opened.
	var goneID string
	if err := s.pool.QueryRow(ctx, `SELECT id::text FROM inventory_items WHERE source_id=$1 AND source_item_id='g0'`, d.ID).Scan(&goneID); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistSnapshot(ctx, d, "http://romm.example.test", inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().Add(time.Second).UTC(), Items: items[1:5]}); err != nil {
		t.Fatal(err)
	}
	after, _ := s.BrowseLibrary(ctx, domain.LibraryQuery{Type: domain.MediaGame, Search: "bfx Quest 0"})
	if len(after.Items) != 0 {
		t.Fatalf("a removed item is still listed: %+v", after.Items)
	}
	if it, err := s.LibraryItemByID(ctx, goneID); err != nil || it.Title != "bfx Quest 0" {
		t.Fatalf("detail of a removed item: %+v %v", it, err)
	}
	if _, err := s.LibraryItemByID(ctx, "nope"); !errors.Is(err, ErrLibraryItemNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestAnAlbumListsItsTracksInOrderIntegration(t *testing.T) {
	ctx, s, _ := setupDraftStore(t)
	d := inventory.Descriptor{ID: newUUID(), Kind: "navidrome", Name: "Track fixture", MediaTypes: []domain.MediaType{domain.MediaAlbum, domain.MediaSong}}
	tag := newUUID()[:8]
	album := "album:" + tag
	snap := inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().UTC(), Items: []inventory.Item{
		{SourceID: d.ID, SourceItemID: album, MediaType: domain.MediaAlbum, Title: "bfx Album " + tag},
		{SourceID: d.ID, SourceItemID: "song:b" + tag, MediaType: domain.MediaSong, Title: "Second", ParentSourceItemID: album, Disc: 1, Position: 2},
		{SourceID: d.ID, SourceItemID: "song:a" + tag, MediaType: domain.MediaSong, Title: "First", ParentSourceItemID: album, Disc: 1, Position: 1},
		{SourceID: d.ID, SourceItemID: "song:c" + tag, MediaType: domain.MediaSong, Title: "Bonus disc", ParentSourceItemID: album, Disc: 2, Position: 1},
		{SourceID: d.ID, SourceItemID: "song:x" + tag, MediaType: domain.MediaSong, Title: "Someone else's", ParentSourceItemID: "album:other"},
	}}
	if err := s.PersistSnapshot(ctx, d, "http://music.example.test", snap); err != nil {
		t.Fatal(err)
	}
	page, err := s.BrowseLibrary(ctx, domain.LibraryQuery{Type: domain.MediaAlbum, Search: "bfx album " + tag})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("browse: %+v %v", page.Items, err)
	}
	if len(page.Items[0].Tracks) != 0 {
		t.Fatal("the list stays light: tracks come with a single item")
	}
	item, err := s.LibraryItemByID(ctx, page.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tr := range item.Tracks {
		got = append(got, tr.Title)
	}
	if fmt.Sprint(got) != "[First Second Bonus disc]" {
		t.Fatalf("tracks = %v", got)
	}
}

// Every field an adapter sets on an inventory.Item must survive PersistSnapshot and reach the
// single-item read; the list leaves the heavy ones out.
func TestLibraryItemDetailsRoundTripIntegration(t *testing.T) {
	ctx, s, _ := setupDraftStore(t)
	d := inventory.Descriptor{ID: newUUID(), Kind: "plex", Name: "Details fixture", MediaTypes: []domain.MediaType{domain.MediaMovie}}
	want := domain.LibraryDetails{Synopsis: "A story.", Rating: 7.6, ContentRating: "PG-13", Genres: []string{"Drama", "Sci-Fi"}, RuntimeMinutes: 116}
	items := []inventory.Item{
		{SourceID: d.ID, SourceItemID: "m1", MediaType: domain.MediaMovie, Title: "dtl With", AddedAt: time.Now().UTC(), Details: want},
		{SourceID: d.ID, SourceItemID: "m2", MediaType: domain.MediaMovie, Title: "dtl Without", AddedAt: time.Now().UTC().Add(-time.Hour)},
	}
	if err := s.PersistSnapshot(ctx, d, "http://plex.example.test", inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().UTC(), Items: items}); err != nil {
		t.Fatal(err)
	}
	page, err := s.BrowseLibrary(ctx, domain.LibraryQuery{Type: domain.MediaMovie, Search: "dtl"})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("browse: %+v err %v", page.Items, err)
	}
	for _, it := range page.Items {
		if it.Details != nil {
			t.Fatalf("the list must not carry details: %+v", it)
		}
	}
	with, err := s.LibraryItemByID(ctx, page.Items[0].ID)
	if err != nil || with.Details == nil || with.Details.Synopsis != want.Synopsis || with.Details.Rating != want.Rating || with.Details.ContentRating != want.ContentRating || with.Details.RuntimeMinutes != want.RuntimeMinutes || len(with.Details.Genres) != 2 {
		t.Fatalf("details = %+v err %v", with.Details, err)
	}
	without, err := s.LibraryItemByID(ctx, page.Items[1].ID)
	if err != nil || without.Details != nil {
		t.Fatalf("an item the library said nothing about has no details: %+v err %v", without.Details, err)
	}
}
