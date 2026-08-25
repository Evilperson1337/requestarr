package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestMediaTypeValid(t *testing.T) {
	for _, mediaType := range []MediaType{MediaMovie, MediaTV, MediaAlbum, MediaSong, MediaGame, MediaAudiobook} {
		if !mediaType.Valid() {
			t.Fatalf("%q should be valid", mediaType)
		}
	}
	if MediaType("book").Valid() || MediaType("").Valid() {
		t.Fatal("unknown media types must be invalid")
	}
}

func TestNormalizeAudiobookDetails(t *testing.T) {
	abridged := true
	got, err := NormalizeAudiobookDetails(AudiobookDetails{
		ASIN:           " b0abc12345 ",
		AudibleRegion:  " us ",
		Authors:        []string{" Ada Lovelace ", "ada lovelace", "Grace Hopper"},
		Narrators:      []string{"First Narrator", "first narrator", "Second Narrator"},
		Series:         " A Series ",
		SeriesPosition: " 2.5 ",
		RuntimeSeconds: 3600,
		Language:       " en ",
		Publisher:      " Publisher ",
		ReleaseDate:    " 2026-08-21 ",
		EditionType:    " full-cast ",
		Abridged:       &abridged,
		AlternateIDs: []AlternateStableID{
			{Namespace: " ISBN13 ", Value: " 9780123456789 "},
			{Namespace: "isbn13", Value: "9780123456789"},
			{Namespace: "audnexus", Value: "edition-1"},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeAudiobookDetails() error = %v", err)
	}
	want := AudiobookDetails{
		ASIN: "B0ABC12345", AudibleRegion: "US", Authors: []string{"Ada Lovelace", "Grace Hopper"},
		Narrators: []string{"First Narrator", "Second Narrator"}, Series: "A Series", SeriesPosition: "2.5",
		RuntimeSeconds: 3600, Language: "en", Publisher: "Publisher", ReleaseDate: "2026-08-21",
		EditionType: "full-cast", Abridged: &abridged,
		AlternateIDs: []AlternateStableID{{Namespace: "isbn13", Value: "9780123456789"}, {Namespace: "audnexus", Value: "edition-1"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeAudiobookDetails() = %#v, want %#v", got, want)
	}
}

func TestNormalizeAudiobookDetailsRejectsUnsafeOrInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		details AudiobookDetails
	}{
		{"ASIN without region", AudiobookDetails{ASIN: "B0ABC12345"}},
		{"region without ASIN", AudiobookDetails{AudibleRegion: "US"}},
		{"invalid ASIN", AudiobookDetails{ASIN: "not-an-asin", AudibleRegion: "US"}},
		{"invalid region", AudiobookDetails{ASIN: "B0ABC12345", AudibleRegion: "USA"}},
		{"invalid UTF-8", AudiobookDetails{Authors: []string{string([]byte{0xff})}}},
		{"control character", AudiobookDetails{Narrators: []string{"narrator\nname"}}},
		{"too many authors", AudiobookDetails{Authors: make([]string, maxAudiobookPeople+1)}},
		{"oversized publisher", AudiobookDetails{Publisher: strings.Repeat("a", 257)}},
		{"invalid date", AudiobookDetails{ReleaseDate: "2026-02-29"}},
		{"runtime too large", AudiobookDetails{RuntimeSeconds: maxAudiobookRuntime + 1}},
		{"invalid alternate namespace", AudiobookDetails{AlternateIDs: []AlternateStableID{{Namespace: "Bad Namespace", Value: "1"}}}},
		{"empty alternate ID", AudiobookDetails{AlternateIDs: []AlternateStableID{{Namespace: "isbn", Value: " "}}}},
		{"too many alternate IDs", AudiobookDetails{AlternateIDs: make([]AlternateStableID, maxAudiobookAlternateIDs+1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NormalizeAudiobookDetails(tt.details); err == nil {
				t.Fatal("NormalizeAudiobookDetails() error = nil")
			}
		})
	}
}

func TestNormalizeAudiobookDetailsRetainsEditionAndNarratorDistinctions(t *testing.T) {
	first, err := NormalizeAudiobookDetails(AudiobookDetails{Narrators: []string{"Narrator One"}, EditionType: "unabridged"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NormalizeAudiobookDetails(AudiobookDetails{Narrators: []string{"Narrator Two"}, EditionType: "dramatized"})
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(first, second) {
		t.Fatal("distinct narrator and edition evidence must be retained")
	}
}
