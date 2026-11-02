package inventory

import (
	"context"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
)

type Item struct {
	SourceID     string
	SourceItemID string
	MediaType    domain.MediaType
	Title        string
	Year         int
	PlatformKey  string
	ExternalIDs  map[string]string
	ExternalURL  string
	LastSeenAt   time.Time
	// AddedAt is when the library added the item, when it says so. Zero otherwise.
	AddedAt time.Time
	// ArtworkRef is the library's own reference to the item's cover, which only
	// that adapter can fetch (ArtworkSource). Empty when it has none.
	ArtworkRef string
	// Detail is a short note the library knows about the item, such as a game's platform name.
	Detail string
}

// Artwork is one fetched cover image.
type Artwork struct {
	ContentType string
	Data        []byte
}

// ArtworkSource is implemented by adapters that can fetch the cover an item's
// ArtworkRef names. The library's credentials never leave the adapter.
type ArtworkSource interface {
	Artwork(ctx context.Context, ref string) (Artwork, error)
}

// Snapshot describes one source's complete inventory view. Complete must only
// be true when the source successfully enumerated every configured item; an
// empty library is a Complete snapshot with no items, which is different from a
// failed enumeration (an error).
type Snapshot struct {
	SourceID   string
	Items      []Item
	Complete   bool
	ObservedAt time.Time
}

// Descriptor is what a Source declares about itself. The Source is
// authoritative for availability only for MediaTypes; Items carry ExternalIDs
// only of IdentifierKinds, named after the catalog provider they match
// (for example "tmdb").
type Descriptor struct {
	ID              string // stable per configured Source
	Kind            string // adapter kind: "plex", "jellyfin", ...
	Name            string // display name
	MediaTypes      []domain.MediaType
	IdentifierKinds []string
}

// Source is the one narrow interface every library adapter implements
// (D015, D019). The shared contract suite in package inventorytest holds an
// adapter to it.
type Source interface {
	Descriptor() Descriptor
	// Snapshot enumerates the Source's inventory for its declared MediaTypes.
	// On success it returns a Complete snapshot; on any failure (transport,
	// authentication, malformed or truncated responses, a later page failing)
	// it returns an error and no items. It never returns partial inventory as
	// if it were complete, and its errors never contain secrets.
	Snapshot(context.Context) (Snapshot, error)
}
