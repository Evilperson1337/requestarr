package domain

import "time"

type MediaType string

const (
	MediaMovie MediaType = "movie"
	MediaTV    MediaType = "tv"
	MediaAlbum MediaType = "album"
	MediaSong  MediaType = "song"
	MediaGame  MediaType = "game"
	// MediaAudiobook is identified by its ASIN (D031).
	MediaAudiobook MediaType = "audiobook"
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

type ResolvedItem struct {
	Item      CatalogItem
	Platforms []Platform
	Movie     *MovieDetails `json:"Movie,omitempty"`
	TV        *TVDetails    `json:"TV,omitempty"`
	Game      *GameDetails  `json:"Game,omitempty"`
	Music     *MusicDetails `json:"Music,omitempty"`
	Preview   *MoviePreview `json:"Preview,omitempty"`
	// Aliases are other identifiers the provider already told us belong to this
	// item, such as every release of an album's release group (D020). Server-side only.
	Aliases []Alias `json:"-"`
}

// Alias is one alternate identifier of a catalog item.
type Alias struct {
	Kind  string
	Value string
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
