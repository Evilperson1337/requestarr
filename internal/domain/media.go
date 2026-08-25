package domain

import "time"

type MediaType string

const (
	MediaMovie     MediaType = "movie"
	MediaTV        MediaType = "tv"
	MediaAlbum     MediaType = "album"
	MediaSong      MediaType = "song"
	MediaGame      MediaType = "game"
	MediaAudiobook MediaType = "audiobook"
)

// Valid reports whether t is a canonical media type supported by Requestarr.
func (t MediaType) Valid() bool {
	switch t {
	case MediaMovie, MediaTV, MediaAlbum, MediaSong, MediaGame, MediaAudiobook:
		return true
	default:
		return false
	}
}

type ArtworkKind string

const (
	ArtworkPoster ArtworkKind = "poster"
	ArtworkSquare ArtworkKind = "square"
)

type CatalogItem struct {
	ID          string
	MediaType   MediaType
	Provider    string
	ProviderID  string
	Title       string
	Year        int
	ArtworkURL  string
	ArtworkKind ArtworkKind
	ExternalURL string
	Metadata    map[string]any
	UpdatedAt   time.Time
}

type ResolvedItem struct {
	Item      CatalogItem
	Platforms []Platform
}

type Platform struct {
	ProviderID string `json:"providerId"`
	Key        string `json:"key"`
	Name       string `json:"name"`
}
