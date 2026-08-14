package inventory

import (
	"context"
	"time"

	"github.com/example/requestarr/internal/domain"
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
}

type ChangeSet struct {
	Added       []Item
	Updated     []Item
	Removed     []Item
	CompletedAt time.Time
}

// Source is authoritative for availability only for media types enabled in its configuration.
type Source interface {
	Name() string
	Sync(context.Context, Cursor) (ChangeSet, Cursor, error)
}

type Cursor struct{ Value string }
