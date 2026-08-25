package inventory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
)

type snapshotRepositoryFake struct{ received AudiobookSnapshot }

func (f *snapshotRepositoryFake) CommitAudiobookSnapshot(_ context.Context, snapshot AudiobookSnapshot) (SnapshotCommit, error) {
	f.received = snapshot
	return SnapshotCommit{SnapshotID: "snapshot", Committed: true}, nil
}

func TestAudiobookSnapshotServiceRequiresCompleteSnapshot(t *testing.T) {
	repository := &snapshotRepositoryFake{}
	service := NewAudiobookSnapshotService(repository)
	_, err := service.Commit(context.Background(), AudiobookSnapshot{SourceID: "source", CompletedAt: time.Now(), Complete: false})
	if !errors.Is(err, ErrSnapshotIncomplete) {
		t.Fatalf("error=%v", err)
	}
	if repository.received.SourceID != "" {
		t.Fatal("repository called for incomplete snapshot")
	}
}

func TestCanonicalAudiobookSnapshotDoesNotMutateInput(t *testing.T) {
	snapshot := AudiobookSnapshot{SelectedLibrary: []RemoteLibrary{{ID: "z"}, {ID: "a"}}, Items: []AudiobookItem{{SourceItemID: "z"}, {SourceItemID: "a"}}}
	canonical := CanonicalAudiobookSnapshot(snapshot)
	if canonical.SelectedLibrary[0].ID != "a" || canonical.Items[0].SourceItemID != "a" {
		t.Fatalf("canonical=%+v", canonical)
	}
	if snapshot.SelectedLibrary[0].ID != "z" || snapshot.Items[0].SourceItemID != "z" {
		t.Fatalf("input mutated=%+v", snapshot)
	}
}

func TestCanonicalAudiobookSnapshotNormalizesContentAndExcludesCompletionTime(t *testing.T) {
	abridged := true
	snapshot := AudiobookSnapshot{
		SourceID:        " source ",
		CompletedAt:     time.Date(2026, 8, 21, 12, 0, 0, 0, time.FixedZone("offset", -4*60*60)),
		SelectedLibrary: []RemoteLibrary{{ID: " library ", Name: " Library "}},
		Items: []AudiobookItem{{
			RemoteLibraryID: " library ", SourceItemID: " book ", Title: " Title ", ExternalURL: " HTTPS://BOOKS.EXAMPLE/Book ",
			Details: domain.AudiobookDetails{ASIN: "b012345678", AudibleRegion: "us", Authors: []string{" Author ", "author"}, Abridged: &abridged},
		}},
	}
	canonical := CanonicalAudiobookSnapshot(snapshot)
	if !canonical.CompletedAt.IsZero() || canonical.SourceID != "source" {
		t.Fatalf("canonical header=%+v", canonical)
	}
	item := canonical.Items[0]
	if item.ExternalURL != "https://books.example/Book" || item.Details.ASIN != "B012345678" || len(item.Details.Authors) != 1 || item.Details.Authors[0] != "Author" {
		t.Fatalf("canonical item=%+v", item)
	}
}

func TestNormalizeExternalURL(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		valid             bool
	}{
		{name: "empty", input: "  ", valid: true},
		{name: "normalizes scheme and host", input: " HTTPS://BOOKS.Example/path?q=1 ", want: "https://books.example/path?q=1", valid: true},
		{name: "relative", input: "/book", valid: false},
		{name: "unsupported scheme", input: "ftp://books.example/book", valid: false},
		{name: "missing host", input: "https:/book", valid: false},
		{name: "credentials", input: "https://user:pass@books.example/book", valid: false},
		{name: "control", input: "https://books.example/\nbook", valid: false},
		{name: "invalid UTF-8", input: string([]byte{0xff}), valid: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeExternalURL(tc.input)
			if tc.valid && err != nil {
				t.Fatalf("NormalizeExternalURL() error=%v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("NormalizeExternalURL() accepted invalid URL")
			}
			if got != tc.want {
				t.Fatalf("NormalizeExternalURL()=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateAudiobookSnapshotRejectsOversizedCollections(t *testing.T) {
	validLibrary := RemoteLibrary{ID: "library", Name: "Library"}
	for _, tc := range []struct {
		name     string
		snapshot AudiobookSnapshot
	}{
		{
			name: "libraries",
			snapshot: AudiobookSnapshot{
				SourceID: "source", CompletedAt: time.Now(), Complete: true,
				SelectedLibrary: make([]RemoteLibrary, maxSelectedLibraries+1),
			},
		},
		{
			name: "items",
			snapshot: AudiobookSnapshot{
				SourceID: "source", CompletedAt: time.Now(), Complete: true,
				SelectedLibrary: []RemoteLibrary{validLibrary}, Items: make([]AudiobookItem, maxAudiobookItems+1),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateAudiobookSnapshot(tc.snapshot); !errors.Is(err, ErrInvalidAudiobookSnapshot) {
				t.Fatalf("ValidateAudiobookSnapshot() error=%v, want invalid snapshot", err)
			}
		})
	}
}

func TestValidateAudiobookSnapshotRejectsUnsafeText(t *testing.T) {
	valid := func() AudiobookSnapshot {
		return AudiobookSnapshot{
			SourceID: "source", CompletedAt: time.Now(), Complete: true,
			SelectedLibrary: []RemoteLibrary{{ID: "library", Name: "Library"}},
			Items:           []AudiobookItem{{RemoteLibraryID: "library", SourceItemID: "item", Title: "Title"}},
		}
	}
	for _, tc := range []struct {
		name  string
		apply func(*AudiobookSnapshot)
	}{
		{name: "source invalid UTF-8", apply: func(s *AudiobookSnapshot) { s.SourceID = string([]byte{0xff}) }},
		{name: "library ID control", apply: func(s *AudiobookSnapshot) { s.SelectedLibrary[0].ID = "library\n" }},
		{name: "library name too long", apply: func(s *AudiobookSnapshot) { s.SelectedLibrary[0].Name = strings.Repeat("x", maxLibraryNameLength+1) }},
		{name: "item ID invalid UTF-8", apply: func(s *AudiobookSnapshot) { s.Items[0].SourceItemID = string([]byte{0xff}) }},
		{name: "title control", apply: func(s *AudiobookSnapshot) { s.Items[0].Title = "Title\x00" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := valid()
			tc.apply(&snapshot)
			if err := ValidateAudiobookSnapshot(snapshot); !errors.Is(err, ErrInvalidAudiobookSnapshot) {
				t.Fatalf("ValidateAudiobookSnapshot() error=%v, want invalid snapshot", err)
			}
		})
	}
}
