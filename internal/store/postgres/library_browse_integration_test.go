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
