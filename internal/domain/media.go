package domain

import "time"

type MediaType string

const (
	MediaMovie MediaType = "movie"
	MediaTV    MediaType = "tv"
	MediaAlbum MediaType = "album"
	MediaSong  MediaType = "song"
	MediaGame  MediaType = "game"
)

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
