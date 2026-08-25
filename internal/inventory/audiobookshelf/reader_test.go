package audiobookshelf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
)

func TestNormalizeFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "complete_document.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document Document
	if err := json.Unmarshal(fixture, &document); err != nil {
		t.Fatal(err)
	}
	requirements := inventory.CompleteReaderRequirements{SourceID: "00000000-0000-4000-8000-000000000001", SelectedLibraries: []string{"library-fiction"}, EnabledMediaTypes: []domain.MediaType{domain.MediaAudiobook}}
	result, err := Normalize(requirements, time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC), document)
	if err != nil {
		t.Fatalf("normalize fixture: %v", err)
	}
	if !result.Complete || len(result.Coverage) != 1 || len(result.Items) != 1 || result.Items[0].MediaType != domain.MediaAudiobook {
		t.Fatalf("result=%+v", result)
	}
}

func TestNormalizeRejectsNonAudiobookScope(t *testing.T) {
	_, err := Normalize(inventory.CompleteReaderRequirements{SourceID: "source", SelectedLibraries: []string{"fiction"}, EnabledMediaTypes: []domain.MediaType{domain.MediaMovie}}, time.Now(), Document{Libraries: []Library{{ID: "fiction", Name: "Fiction"}}})
	if err == nil {
		t.Fatalf("error=%v", err)
	}
}

func TestNormalizeDoesNotIncludeUntrustedItemIDInError(t *testing.T) {
	itemID := "item\nwith-control"
	_, err := Normalize(inventory.CompleteReaderRequirements{SourceID: "source", SelectedLibraries: []string{"library"}, EnabledMediaTypes: []domain.MediaType{domain.MediaAudiobook}}, time.Now(), Document{
		Libraries: []Library{{ID: "library", Name: "Library"}},
		Items:     []Item{{LibraryID: "library", ID: itemID, Title: "Title"}},
	})
	if err == nil {
		t.Fatal("Normalize() accepted invalid item ID")
	}
	if strings.Contains(err.Error(), itemID) {
		t.Fatalf("Normalize() error contains untrusted item ID: %q", err)
	}
}
