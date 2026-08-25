package inventory

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
)

var (
	ErrInvalidAudiobookSnapshot = errors.New("invalid audiobook inventory snapshot")
	ErrSnapshotIncomplete       = errors.New("audiobook inventory snapshot is incomplete")
)

const (
	maxSelectedLibraries = 100
	maxAudiobookItems    = 10_000
	maxSourceIDLength    = 256
	maxLibraryIDLength   = 256
	maxLibraryNameLength = 512
	maxItemIDLength      = 256
	maxTitleLength       = 512
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

// RemoteLibrary is a source-native Audiobookshelf library observed during a
// complete enumeration. Its ID is opaque: interpretation belongs to the reader
// adapter rather than inventory. Selection configuration is intentionally not
// modeled here.
type RemoteLibrary struct {
	ID   string
	Name string
}

// AudiobookItem is the bounded audiobook projection retained by inventory.
// It deliberately carries normalized details, rather than source payloads.
type AudiobookItem struct {
	RemoteLibraryID string
	SourceItemID    string
	Title           string
	ExternalURL     string
	Details         domain.AudiobookDetails
}

// AudiobookSnapshot represents one complete enumeration of all selected remote
// libraries for a source. Incomplete results must not be committed, because a
// partial list could incorrectly tombstone known inventory.
type AudiobookSnapshot struct {
	SourceID        string
	CompletedAt     time.Time
	SelectedLibrary []RemoteLibrary
	Items           []AudiobookItem
	Complete        bool
}

// SnapshotCommit reports whether a successful complete snapshot was new.
type SnapshotCommit struct {
	SnapshotID string
	Committed  bool
}

// AudiobookSnapshotRepository persists local authoritative state. It is kept
// independent from source readers so callers can run readers and storage on
// separate schedules without exposing an external API at this layer.
type AudiobookSnapshotRepository interface {
	CommitAudiobookSnapshot(context.Context, AudiobookSnapshot) (SnapshotCommit, error)
}

// AudiobookSnapshotService validates a complete reader result before it reaches
// persistence. It intentionally does not invoke readers, schedulers, or HTTP.
type AudiobookSnapshotService struct{ repository AudiobookSnapshotRepository }

func NewAudiobookSnapshotService(repository AudiobookSnapshotRepository) *AudiobookSnapshotService {
	return &AudiobookSnapshotService{repository: repository}
}

func (s *AudiobookSnapshotService) Commit(ctx context.Context, snapshot AudiobookSnapshot) (SnapshotCommit, error) {
	if err := ValidateAudiobookSnapshot(snapshot); err != nil {
		return SnapshotCommit{}, err
	}
	if s == nil || s.repository == nil {
		return SnapshotCommit{}, fmt.Errorf("%w: repository is required", ErrInvalidAudiobookSnapshot)
	}
	return s.repository.CommitAudiobookSnapshot(ctx, snapshot)
}

// ValidateAudiobookSnapshot enforces the local, complete-snapshot contract.
func ValidateAudiobookSnapshot(snapshot AudiobookSnapshot) error {
	if !snapshot.Complete {
		return ErrSnapshotIncomplete
	}
	if len(snapshot.SelectedLibrary) > maxSelectedLibraries {
		return fmt.Errorf("%w: too many selected libraries", ErrInvalidAudiobookSnapshot)
	}
	if len(snapshot.Items) > maxAudiobookItems {
		return fmt.Errorf("%w: too many audiobook items", ErrInvalidAudiobookSnapshot)
	}
	if !validSnapshotText(snapshot.SourceID, maxSourceIDLength) || snapshot.CompletedAt.IsZero() || len(snapshot.SelectedLibrary) == 0 {
		return fmt.Errorf("%w: source, completion time, and selected libraries are required", ErrInvalidAudiobookSnapshot)
	}
	libraries := make(map[string]struct{}, len(snapshot.SelectedLibrary))
	for _, library := range snapshot.SelectedLibrary {
		if !validSnapshotText(library.ID, maxLibraryIDLength) || !validSnapshotText(library.Name, maxLibraryNameLength) {
			return fmt.Errorf("%w: invalid selected library", ErrInvalidAudiobookSnapshot)
		}
		library.ID, library.Name = strings.TrimSpace(library.ID), strings.TrimSpace(library.Name)
		if _, exists := libraries[library.ID]; exists {
			return fmt.Errorf("%w: duplicate selected library", ErrInvalidAudiobookSnapshot)
		}
		libraries[library.ID] = struct{}{}
	}
	items := make(map[string]struct{}, len(snapshot.Items))
	for _, item := range snapshot.Items {
		if !validSnapshotText(item.RemoteLibraryID, maxLibraryIDLength) || !validSnapshotText(item.SourceItemID, maxItemIDLength) || !validSnapshotText(item.Title, maxTitleLength) || len(item.ExternalURL) > 2048 {
			return fmt.Errorf("%w: invalid audiobook item", ErrInvalidAudiobookSnapshot)
		}
		item.RemoteLibraryID, item.SourceItemID, item.Title = strings.TrimSpace(item.RemoteLibraryID), strings.TrimSpace(item.SourceItemID), strings.TrimSpace(item.Title)
		if _, ok := libraries[item.RemoteLibraryID]; !ok {
			return fmt.Errorf("%w: invalid audiobook item", ErrInvalidAudiobookSnapshot)
		}
		if _, err := NormalizeExternalURL(item.ExternalURL); err != nil {
			return fmt.Errorf("%w: external URL: %v", ErrInvalidAudiobookSnapshot, err)
		}
		if _, exists := items[item.SourceItemID]; exists {
			return fmt.Errorf("%w: duplicate source item", ErrInvalidAudiobookSnapshot)
		}
		if _, err := domain.NormalizeAudiobookDetails(item.Details); err != nil {
			return fmt.Errorf("%w: audiobook details: %v", ErrInvalidAudiobookSnapshot, err)
		}
		items[item.SourceItemID] = struct{}{}
	}
	return nil
}

func validSnapshotText(value string, maxLength int) bool {
	if !utf8.ValidString(value) || len(value) > maxLength || strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// CanonicalAudiobookSnapshot returns a deterministic copy used for repository
// idempotency fingerprints; callers' slices are never reordered.
func CanonicalAudiobookSnapshot(snapshot AudiobookSnapshot) AudiobookSnapshot {
	copy := snapshot
	copy.CompletedAt = time.Time{}
	copy.SelectedLibrary = append([]RemoteLibrary(nil), snapshot.SelectedLibrary...)
	copy.Items = append([]AudiobookItem(nil), snapshot.Items...)
	copy.SourceID = strings.TrimSpace(copy.SourceID)
	for i := range copy.SelectedLibrary {
		copy.SelectedLibrary[i].ID = strings.TrimSpace(copy.SelectedLibrary[i].ID)
		copy.SelectedLibrary[i].Name = strings.TrimSpace(copy.SelectedLibrary[i].Name)
	}
	for i := range copy.Items {
		item := &copy.Items[i]
		item.RemoteLibraryID = strings.TrimSpace(item.RemoteLibraryID)
		item.SourceItemID = strings.TrimSpace(item.SourceItemID)
		item.Title = strings.TrimSpace(item.Title)
		item.ExternalURL, _ = NormalizeExternalURL(item.ExternalURL)
		item.Details, _ = domain.NormalizeAudiobookDetails(item.Details)
	}
	sort.Slice(copy.SelectedLibrary, func(i, j int) bool { return copy.SelectedLibrary[i].ID < copy.SelectedLibrary[j].ID })
	sort.Slice(copy.Items, func(i, j int) bool { return copy.Items[i].SourceItemID < copy.Items[j].SourceItemID })
	return copy
}

// NormalizeExternalURL validates an optional public HTTP(S) URL and returns its
// trimmed, canonical representation. Inventory links are display metadata, so
// credentials, relative URLs, malformed UTF-8, and control characters are not
// accepted from untrusted source payloads.
func NormalizeExternalURL(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("must be valid UTF-8")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", errors.New("contains control characters")
		}
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.Hostname() == "" {
		return "", errors.New("must be an absolute URL with a host")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return "", errors.New("must use http or https")
	}
	if parsed.User != nil {
		return "", errors.New("must not contain user credentials")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String(), nil
}
