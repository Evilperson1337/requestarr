package domain

import (
	"math"
	"strings"
	"time"
)

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

// LibraryDetails is what a library says about an item: the facts the Library dialog shows.
// Every field is optional because libraries differ in what they know.
type LibraryDetails struct {
	Synopsis string `json:"synopsis,omitempty"`
	// Rating is the library's audience or community rating on a 0 to 10 scale.
	Rating float64 `json:"rating,omitempty"`
	// ContentRating is a certification such as PG-13.
	ContentRating  string   `json:"contentRating,omitempty"`
	Genres         []string `json:"genres,omitempty"`
	RuntimeMinutes int      `json:"runtimeMinutes,omitempty"`
}

const (
	maxSynopsisRunes = 4000
	maxGenres        = 8
	maxDetailRunes   = 64
)

// Clean drops anything out of range and shortens what is too long, so a library
// can never store or serve more than the dialog can sensibly show.
func (d LibraryDetails) Clean() LibraryDetails {
	short := func(s string, n int) string {
		s = strings.TrimSpace(strings.Map(func(r rune) rune {
			if r == '\x00' || (r < 32 && r != '\n') {
				return -1
			}
			return r
		}, s))
		if r := []rune(s); len(r) > n {
			return strings.TrimSpace(string(r[:n]))
		}
		return s
	}
	out := LibraryDetails{Synopsis: short(d.Synopsis, maxSynopsisRunes), ContentRating: short(d.ContentRating, 16)}
	if d.Rating > 0 && d.Rating <= 10 {
		out.Rating = math.Round(d.Rating*10) / 10
	}
	if d.RuntimeMinutes > 0 && d.RuntimeMinutes <= 10000 {
		out.RuntimeMinutes = d.RuntimeMinutes
	}
	for _, g := range d.Genres {
		if g = short(g, maxDetailRunes); g != "" && len(out.Genres) < maxGenres {
			out.Genres = append(out.Genres, g)
		}
	}
	return out
}

// Empty reports whether the library said nothing worth showing.
func (d LibraryDetails) Empty() bool {
	return d.Synopsis == "" && d.Rating == 0 && d.ContentRating == "" && len(d.Genres) == 0 && d.RuntimeMinutes == 0
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
	// IssueKinds are the kinds of Issue that can be reported about this item.
	IssueKinds []IssueKindOption `json:"issueKinds"`
	// Details is what the library says about the item. Only a single item has them.
	Details *LibraryDetails `json:"details,omitempty"`
	// Tracks lists an album's tracks, when its library reports them. Only a single item has them.
	Tracks []LibraryTrack `json:"tracks,omitempty"`
}

// LibraryTrack is one track of an album a Source holds.
type LibraryTrack struct {
	// ID is the track's Library item id, which is what a report names.
	ID       string `json:"id"`
	Title    string `json:"title"`
	Disc     int    `json:"disc,omitempty"`
	Position int    `json:"position,omitempty"`
}

// IssueKindOption is one choice in the report form.
type IssueKindOption struct {
	Kind  IssueKind `json:"kind"`
	Label string    `json:"label"`
}

// IssueKindOptions lists the kinds offered for a media type, for the report form.
func IssueKindOptions(media MediaType) []IssueKindOption {
	out := []IssueKindOption{}
	for _, k := range IssueKindsFor(media) {
		out = append(out, IssueKindOption{Kind: k, Label: k.Label()})
	}
	return out
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
