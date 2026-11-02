package domain

import "time"

// RecentLibraryItem is something a library recently gained, as the Home page shows it.
type RecentLibraryItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Year      int       `json:"year,omitempty"`
	MediaType MediaType `json:"mediaType"`
	// Source is the library's name, such as Plex.
	Source  string    `json:"source"`
	AddedAt time.Time `json:"addedAt"`
	// ArtworkURL is set when the library can supply a cover for the item.
	ArtworkURL string `json:"artworkUrl,omitempty"`
}
