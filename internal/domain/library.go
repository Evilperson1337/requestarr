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
	// Detail is a short note from the library, such as a game's platform.
	Detail string `json:"detail,omitempty"`
	// ArtworkURL is set when the library can supply a cover for the item.
	ArtworkURL string `json:"artworkUrl,omitempty"`
}

// LibraryItem is one thing a Source holds, as the Library page lists and opens it.
type LibraryItem struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Year       int       `json:"year,omitempty"`
	MediaType  MediaType `json:"mediaType"`
	Source     string    `json:"source"`
	SourceKind string    `json:"sourceKind"`
	AddedAt    time.Time `json:"addedAt"`
	// Detail is a short note from the library, such as a game's platform.
	Detail     string `json:"detail,omitempty"`
	ArtworkURL string `json:"artworkUrl,omitempty"`
	// Link opens the item in its media server, when the library supplies a safe one.
	Link string `json:"link,omitempty"`
	// OpenIssues counts the Issues about this item that are still open.
	OpenIssues int `json:"openIssues"`
}

// LibraryQuery filters the Library page. Type is required.
type LibraryQuery struct {
	Type   MediaType
	Search string
	// Source is a Source kind slug such as "plex"; empty means all.
	Source string
	Limit  int
	// After is the cursor from the previous page.
	After string
}

type LibraryPage struct {
	Items      []LibraryItem `json:"items"`
	NextCursor *string       `json:"nextCursor"`
}
