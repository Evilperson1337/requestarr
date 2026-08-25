package postgres

import (
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
)

func TestSnapshotHashUsesCanonicalContentRatherThanCompletionTime(t *testing.T) {
	base := inventory.AudiobookSnapshot{
		SourceID:        "source",
		CompletedAt:     time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		Complete:        true,
		SelectedLibrary: []inventory.RemoteLibrary{{ID: "library", Name: "Library"}},
		Items:           []inventory.AudiobookItem{{RemoteLibraryID: "library", SourceItemID: "book", Title: "Title", ExternalURL: "HTTPS://BOOKS.EXAMPLE/book", Details: domain.AudiobookDetails{ASIN: "b012345678", AudibleRegion: "us", Authors: []string{" Author "}}}},
	}
	changedTime := base
	changedTime.CompletedAt = base.CompletedAt.Add(time.Hour)
	changedFormatting := base
	changedFormatting.Items = append([]inventory.AudiobookItem(nil), base.Items...)
	changedFormatting.Items[0].Details = domain.AudiobookDetails{ASIN: "B012345678", AudibleRegion: "US", Authors: []string{"Author"}}
	changedFormatting.Items[0].ExternalURL = "https://books.example/book"

	want, err := snapshotHash(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range []inventory.AudiobookSnapshot{changedTime, changedFormatting} {
		got, err := snapshotHash(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("hash=%s, want %s", got, want)
		}
	}
}
