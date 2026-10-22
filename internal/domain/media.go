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

// Availability is independent of request lifecycle status.
type Availability string

const (
	AvailabilityUnknown   Availability = "unknown"
	AvailabilityAvailable Availability = "available"
	AvailabilityMissing   Availability = "missing"
)

// AvailabilityEvidence records a source-local exact identity observation.
type AvailabilityEvidence struct {
	CatalogItemID string
	SourceID      string
	SourceItemID  string
	ObservedAt    time.Time
}

// SourceSyncStatus is sanitized operational health for an availability source.
type SourceSyncStatus struct {
	Configured          bool       `json:"configured"`
	Enabled             bool       `json:"enabled"`
	LastSyncStartedAt   *time.Time `json:"lastSyncStartedAt"`
	LastSyncSucceededAt *time.Time `json:"lastSyncSucceededAt"`
	LastSyncError       *string    `json:"lastSyncError"`
}

type ResolvedItem struct {
	Item      CatalogItem
	Platforms []Platform
}

type CatalogPlatform struct {
	CatalogItemID string
	Platform      Platform
}

type Platform struct {
	ProviderID string `json:"providerId"`
	Key        string `json:"key"`
	Name       string `json:"name"`
}

// CatalogRef names a catalog item by its canonical provider identity.
type CatalogRef struct {
	ID         string
	MediaType  MediaType
	Provider   string
	ProviderID string
}
