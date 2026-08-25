package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
)

var ErrInvalidCompleteReaderResult = errors.New("invalid complete inventory reader result")

const maxCompleteReaderItems = 10_000

// CompleteReaderItem is source-observed item data from one selected remote
// library. It is deliberately not an InventoryEvent or AuthoritativeCycle:
// readers cannot express removals and cannot commit inventory state.
type CompleteReaderItem struct {
	RemoteLibraryID string
	SourceItemID    string
	MediaType       domain.MediaType
	Title           string
	Year            int
	ExternalIDs     map[string]string
	ExternalURL     string
}

// CompleteReaderResult is an in-memory, fully enumerated input for a future
// projection slice. Complete must be proven by the adapter before validation.
type CompleteReaderResult struct {
	SourceID    string
	CompletedAt time.Time
	Complete    bool
	// Coverage proves the exact selected-library × enabled-media-type partition
	// enumerated by the reader. It must contain every requested pair once.
	Coverage []ObservationCoverage
	Items    []CompleteReaderItem
}

// CompleteReaderRequirements are trusted configured constraints supplied to a
// reader result validator. They are intentionally independent of persistence.
type CompleteReaderRequirements struct {
	SourceID          string
	SelectedLibraries []string
	EnabledMediaTypes []domain.MediaType
}

// ValidateCompleteReaderResult verifies an in-memory complete enumeration. It
// cannot establish a cycle, produce events, or imply that absent items were
// removed; a later projection must make those decisions explicitly.
func ValidateCompleteReaderResult(result CompleteReaderResult, requirements CompleteReaderRequirements) error {
	if !result.Complete {
		return fmt.Errorf("%w: result is incomplete", ErrInvalidCompleteReaderResult)
	}
	if !validSnapshotText(result.SourceID, maxSourceIDLength) || strings.TrimSpace(result.SourceID) != strings.TrimSpace(requirements.SourceID) || result.CompletedAt.IsZero() {
		return fmt.Errorf("%w: source and completion time are required", ErrInvalidCompleteReaderResult)
	}
	if len(result.Items) > maxCompleteReaderItems || len(requirements.SelectedLibraries) == 0 || len(requirements.SelectedLibraries) > maxSelectedLibraries || len(requirements.EnabledMediaTypes) == 0 || len(requirements.EnabledMediaTypes) > maxConfiguredMediaTypes {
		return fmt.Errorf("%w: result constraints are invalid", ErrInvalidCompleteReaderResult)
	}
	libraries := make(map[string]struct{}, len(requirements.SelectedLibraries))
	for _, id := range requirements.SelectedLibraries {
		id = strings.TrimSpace(id)
		if !validSnapshotText(id, maxLibraryIDLength) {
			return fmt.Errorf("%w: selected library is invalid", ErrInvalidCompleteReaderResult)
		}
		if _, duplicate := libraries[id]; duplicate {
			return fmt.Errorf("%w: selected library is duplicated", ErrInvalidCompleteReaderResult)
		}
		libraries[id] = struct{}{}
	}
	types := make(map[domain.MediaType]struct{}, len(requirements.EnabledMediaTypes))
	for _, mediaType := range requirements.EnabledMediaTypes {
		if !mediaType.Valid() {
			return fmt.Errorf("%w: enabled media type is invalid", ErrInvalidCompleteReaderResult)
		}
		if _, duplicate := types[mediaType]; duplicate {
			return fmt.Errorf("%w: enabled media type is duplicated", ErrInvalidCompleteReaderResult)
		}
		types[mediaType] = struct{}{}
	}
	if err := validateReaderCoverage(libraries, types, result.Coverage); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCompleteReaderResult, err)
	}
	coverage := make(map[coveragePair]CoverageStatus, len(result.Coverage))
	for _, entry := range result.Coverage {
		coverage[coveragePair{libraryID: entry.RemoteLibraryID, mediaType: entry.MediaType}] = entry.Status
	}
	items := make(map[readerItemIdentity]struct{}, len(result.Items))
	for _, item := range result.Items {
		item.RemoteLibraryID = strings.TrimSpace(item.RemoteLibraryID)
		item.SourceItemID = strings.TrimSpace(item.SourceItemID)
		if !validSnapshotText(item.RemoteLibraryID, maxLibraryIDLength) || !validSnapshotText(item.SourceItemID, maxItemIDLength) || !validSnapshotText(item.Title, maxTitleLength) || item.Year < 0 || item.Year > 9999 || len(item.ExternalIDs) > 20 {
			return fmt.Errorf("%w: item is invalid", ErrInvalidCompleteReaderResult)
		}
		if _, selected := libraries[item.RemoteLibraryID]; !selected {
			return fmt.Errorf("%w: item library is not selected", ErrInvalidCompleteReaderResult)
		}
		if _, enabled := types[item.MediaType]; !enabled {
			return fmt.Errorf("%w: item media type is not enabled", ErrInvalidCompleteReaderResult)
		}
		if coverage[coveragePair{libraryID: item.RemoteLibraryID, mediaType: item.MediaType}] != CoverageVerifiedComplete {
			return fmt.Errorf("%w: item has no verified complete coverage", ErrInvalidCompleteReaderResult)
		}
		identity := readerItemIdentity{libraryID: item.RemoteLibraryID, sourceItemID: item.SourceItemID}
		if _, duplicate := items[identity]; duplicate {
			return fmt.Errorf("%w: source item is duplicated", ErrInvalidCompleteReaderResult)
		}
		for provider, id := range item.ExternalIDs {
			if !validSnapshotText(provider, 64) || !validSnapshotText(id, 512) {
				return fmt.Errorf("%w: external ID is invalid", ErrInvalidCompleteReaderResult)
			}
		}
		if len(item.ExternalURL) > 2048 {
			return fmt.Errorf("%w: external URL is invalid", ErrInvalidCompleteReaderResult)
		}
		if _, err := NormalizeExternalURL(item.ExternalURL); err != nil {
			return fmt.Errorf("%w: external URL is invalid", ErrInvalidCompleteReaderResult)
		}
		items[identity] = struct{}{}
	}
	return nil
}

type readerItemIdentity struct {
	libraryID    string
	sourceItemID string
}

// ConvertCompleteReaderResult converts a reader's in-memory result into the
// immutable canonical observation that may later cross an acceptance boundary.
// The caller supplies the trusted persisted scope and source kind; no reader
// controlled field can enlarge either. This function performs no I/O.
func ConvertCompleteReaderResult(scope ObservationScope, kind SourceKind, result CompleteReaderResult) (CompleteObservation, error) {
	if err := validateObservationScope(scope); err != nil {
		return CompleteObservation{}, fmt.Errorf("%w: trusted scope: %v", ErrInvalidCompleteReaderResult, err)
	}
	if !configuredSourceKind(kind) || !sourceKindAllowsMediaTypes(kind, scope.EnabledMediaTypes) {
		return CompleteObservation{}, fmt.Errorf("%w: source kind is incompatible with scope media types", ErrInvalidCompleteReaderResult)
	}
	requirements := CompleteReaderRequirements{SourceID: scope.SourceID, SelectedLibraries: append([]string(nil), scope.SelectedLibraries...), EnabledMediaTypes: append([]domain.MediaType(nil), scope.EnabledMediaTypes...)}
	if err := ValidateCompleteReaderResult(result, requirements); err != nil {
		return CompleteObservation{}, err
	}

	observation := CompleteObservation{
		Scope:    cloneObservationScope(scope),
		Coverage: append([]ObservationCoverage(nil), result.Coverage...),
		Items:    make([]ObservedItem, 0, len(result.Items)),
	}
	for _, item := range result.Items {
		url, err := NormalizeExternalURL(item.ExternalURL)
		if err != nil {
			return CompleteObservation{}, fmt.Errorf("%w: normalize item URL: %v", ErrInvalidCompleteReaderResult, err)
		}
		observed := ObservedItem{RemoteLibraryID: item.RemoteLibraryID, ItemID: item.SourceItemID, MediaType: item.MediaType, Title: item.Title, ExternalURL: url}
		observed.Revision = observedItemRevision(observed)
		observation.Items = append(observation.Items, observed)
	}
	if err := ValidateCompleteObservation(observation); err != nil {
		return CompleteObservation{}, fmt.Errorf("%w: converted observation: %v", ErrInvalidCompleteReaderResult, err)
	}
	return observation, nil
}

func validateReaderCoverage(libraries map[string]struct{}, types map[domain.MediaType]struct{}, coverage []ObservationCoverage) error {
	if len(coverage) != len(libraries)*len(types) {
		return errors.New("coverage does not prove every selected library and enabled media type")
	}
	seen := make(map[coveragePair]struct{}, len(coverage))
	for _, entry := range coverage {
		if !validSnapshotText(entry.RemoteLibraryID, maxLibraryIDLength) || entry.RemoteLibraryID != strings.TrimSpace(entry.RemoteLibraryID) {
			return errors.New("invalid coverage library")
		}
		if _, ok := libraries[entry.RemoteLibraryID]; !ok {
			return errors.New("coverage library is outside selected scope")
		}
		if _, ok := types[entry.MediaType]; !ok || (entry.Status != CoverageVerifiedComplete && entry.Status != CoverageInapplicable) {
			return errors.New("invalid coverage media type or status")
		}
		pair := coveragePair{libraryID: entry.RemoteLibraryID, mediaType: entry.MediaType}
		if _, duplicate := seen[pair]; duplicate {
			return errors.New("duplicate coverage pair")
		}
		seen[pair] = struct{}{}
	}
	return nil
}

func observedItemRevision(item ObservedItem) string {
	payload := strings.Join([]string{item.RemoteLibraryID, item.ItemID, string(item.MediaType), item.Title, item.ExternalURL}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func cloneObservationScope(scope ObservationScope) ObservationScope {
	return ObservationScope{SourceID: scope.SourceID, ConfigurationRevision: scope.ConfigurationRevision, SelectedLibraries: append([]string(nil), scope.SelectedLibraries...), EnabledMediaTypes: append([]domain.MediaType(nil), scope.EnabledMediaTypes...)}
}
