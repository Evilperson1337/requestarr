package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
)

// These errors classify pure observation failures. Callers may use errors.Is
// without parsing validation details.
var (
	ErrInvalidCompleteObservation = errors.New("invalid complete inventory observation")
	ErrObservationIncomplete      = errors.New("inventory observation is not complete and verified")
	ErrInvalidKnownInventoryState = errors.New("invalid known inventory state")
	ErrObservationScopeMismatch   = errors.New("inventory observation scope mismatch")
	ErrInvalidObservationCommit   = errors.New("invalid accepted inventory observation commit")
	ErrObservationCommitConflict  = errors.New("accepted inventory observation replay conflict")
)

const (
	maxObservationItems = 10_000
	maxItemRevisionSize = 256
	maxItemURLSize      = 2048
)

// ObservationScope identifies the exact configured source projection being
// enumerated. ConfigurationRevision prevents results read under an older
// selection from removing items in a newer selection.
type ObservationScope struct {
	SourceID              string
	ConfigurationRevision int64
	SelectedLibraries     []string
	EnabledMediaTypes     []domain.MediaType
}

// SourceLocalIdentity is the canonical identity for a source item. Item IDs
// are only unique inside a remote library, and remote library IDs are only
// unique inside a configured source.
type SourceLocalIdentity struct {
	SourceID        string
	RemoteLibraryID string
	ItemID          string
}

// ObservedItem is the bounded, source-local state returned by a complete
// enumeration. All fields participate in equality, so a changed title, type,
// URL, or revision is an update.
type ObservedItem struct {
	RemoteLibraryID string
	ItemID          string
	MediaType       domain.MediaType
	Title           string
	ExternalURL     string
	Revision        string
}

// CoverageStatus describes the authoritative result for one selected library
// and enabled media-type pair. Only CoverageVerifiedComplete and
// CoverageInapplicable are acceptable in a complete observation.
type CoverageStatus string

const (
	CoverageVerifiedComplete CoverageStatus = "verified_complete"
	CoverageInapplicable     CoverageStatus = "inapplicable"
)

// ObservationCoverage proves the result for one exact library and media-type
// pair. Every pair in Scope must appear exactly once.
type ObservationCoverage struct {
	RemoteLibraryID string
	MediaType       domain.MediaType
	Status          CoverageStatus
}

// CompleteObservation is a full enumeration of exactly Scope. Coverage is an
// explicit proof partition rather than a caller-asserted boolean: every
// selected library and enabled media-type pair is either enumerated completely
// or explicitly inapplicable.
type CompleteObservation struct {
	Scope    ObservationScope
	Coverage []ObservationCoverage
	Items    []ObservedItem
}

// KnownInventoryState is the previously committed local state for exactly one
// configured source scope. It has no persistence semantics.
type KnownInventoryState struct {
	Scope ObservationScope
	Items []ObservedItem
}

// ObservationDiff is deterministic and pairwise-disjoint. Every returned
// slice is sorted by source-local identity and contains copies, never aliases
// caller-owned slices.
type ObservationDiff struct {
	Unchanged []ObservedItem
	Added     []ObservedItem
	Updated   []ObservedItem
	Removed   []ObservedItem
}

// AcceptedObservationCommit is the durable command for accepting an already
// validated full observation. ID identifies one semantic command: reuse with
// different observation content is a conflict, never a second acceptance.
// Acceptance time is server-assigned audit metadata and is not command identity.
type AcceptedObservationCommit struct {
	ID          string
	Observation CompleteObservation
}

// AcceptedObservationCommitResult reports whether the desired-state projection
// changed. Replay returns the original result with Applied false.
type AcceptedObservationCommitResult struct {
	ID      string
	Applied bool
	Added   int
	Updated int
	Removed int
}

// AcceptedObservationRepository atomically stores immutable evidence and
// applies the source-revision desired-state projection.
type AcceptedObservationRepository interface {
	CommitAcceptedObservation(context.Context, AcceptedObservationCommit) (AcceptedObservationCommitResult, error)
}

// AcceptedObservationService is intentionally pure of readers, jobs, and HTTP.
type AcceptedObservationService struct{ repository AcceptedObservationRepository }

func NewAcceptedObservationService(repository AcceptedObservationRepository) *AcceptedObservationService {
	return &AcceptedObservationService{repository: repository}
}

func (s *AcceptedObservationService) Commit(ctx context.Context, command AcceptedObservationCommit) (AcceptedObservationCommitResult, error) {
	if err := ValidateAcceptedObservationCommit(command); err != nil {
		return AcceptedObservationCommitResult{}, err
	}
	if s == nil || s.repository == nil {
		return AcceptedObservationCommitResult{}, fmt.Errorf("%w: repository is required", ErrInvalidObservationCommit)
	}
	return s.repository.CommitAcceptedObservation(ctx, command)
}

// ValidateAcceptedObservationCommit permits only a proven complete observation
// to cross the durable acceptance boundary.
func ValidateAcceptedObservationCommit(command AcceptedObservationCommit) error {
	if !validDurableID(command.ID) {
		return fmt.Errorf("%w: command ID is required", ErrInvalidObservationCommit)
	}
	if err := ValidateCompleteObservation(command.Observation); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidObservationCommit, err)
	}
	return nil
}

// AcceptedObservationFingerprint returns a deterministic SHA-256 identity for
// semantic command content. Input ordering does not affect the fingerprint.
func AcceptedObservationFingerprint(command AcceptedObservationCommit) ([32]byte, error) {
	if err := ValidateAcceptedObservationCommit(command); err != nil {
		return [32]byte{}, err
	}
	type scope struct {
		SourceID   string             `json:"source_id"`
		Revision   int64              `json:"configuration_revision"`
		Libraries  []string           `json:"selected_libraries"`
		MediaTypes []domain.MediaType `json:"enabled_media_types"`
	}
	type coverage struct {
		Library string           `json:"remote_library_id"`
		Type    domain.MediaType `json:"media_type"`
		Status  CoverageStatus   `json:"status"`
	}
	type item struct {
		Library  string           `json:"remote_library_id"`
		ID       string           `json:"source_item_id"`
		Type     domain.MediaType `json:"media_type"`
		Title    string           `json:"title"`
		URL      string           `json:"external_url"`
		Revision string           `json:"revision"`
	}
	canonicalScope := scope{SourceID: command.Observation.Scope.SourceID, Revision: command.Observation.Scope.ConfigurationRevision, Libraries: append([]string(nil), command.Observation.Scope.SelectedLibraries...), MediaTypes: append([]domain.MediaType(nil), command.Observation.Scope.EnabledMediaTypes...)}
	sort.Strings(canonicalScope.Libraries)
	sort.Slice(canonicalScope.MediaTypes, func(i, j int) bool { return canonicalScope.MediaTypes[i] < canonicalScope.MediaTypes[j] })
	coverages := make([]coverage, 0, len(command.Observation.Coverage))
	for _, value := range command.Observation.Coverage {
		coverages = append(coverages, coverage{value.RemoteLibraryID, value.MediaType, value.Status})
	}
	sort.Slice(coverages, func(i, j int) bool {
		if coverages[i].Library != coverages[j].Library {
			return coverages[i].Library < coverages[j].Library
		}
		return coverages[i].Type < coverages[j].Type
	})
	items := make([]item, 0, len(command.Observation.Items))
	for _, value := range command.Observation.Items {
		items = append(items, item{value.RemoteLibraryID, value.ItemID, value.MediaType, value.Title, value.ExternalURL, value.Revision})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Library != items[j].Library {
			return items[i].Library < items[j].Library
		}
		return items[i].ID < items[j].ID
	})
	payload, err := json.Marshal(struct {
		Scope    scope      `json:"scope"`
		Coverage []coverage `json:"coverage"`
		Items    []item     `json:"items"`
	}{canonicalScope, coverages, items})
	if err != nil {
		return [32]byte{}, fmt.Errorf("%w: marshal canonical command: %v", ErrInvalidObservationCommit, err)
	}
	return sha256.Sum256(payload), nil
}

// Identity returns an item's canonical source-local identity within scope.
func (scope ObservationScope) Identity(item ObservedItem) SourceLocalIdentity {
	return SourceLocalIdentity{
		SourceID:        strings.TrimSpace(scope.SourceID),
		RemoteLibraryID: strings.TrimSpace(item.RemoteLibraryID),
		ItemID:          strings.TrimSpace(item.ItemID),
	}
}

// ValidateCompleteObservation rejects anything that cannot prove a bounded,
// complete configured scope. In particular, an empty item list can support
// removals only when Coverage proves every scope pair complete or inapplicable.
func ValidateCompleteObservation(observation CompleteObservation) error {
	if err := validateObservationScope(observation.Scope); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCompleteObservation, err)
	}
	if err := validateObservationCoverage(observation.Scope, observation.Coverage); err != nil {
		return fmt.Errorf("%w: %v", ErrObservationIncomplete, err)
	}
	if err := validateObservationItems(observation.Scope, observation.Items); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCompleteObservation, err)
	}
	if err := validateItemsCovered(observation.Items, observation.Coverage); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCompleteObservation, err)
	}
	return nil
}

// ValidateKnownInventoryState checks that known data is internally consistent
// before it is used to derive removals.
func ValidateKnownInventoryState(state KnownInventoryState) error {
	if err := validateObservationScope(state.Scope); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidKnownInventoryState, err)
	}
	if err := validateObservationItems(state.Scope, state.Items); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidKnownInventoryState, err)
	}
	return nil
}

// DiffCompleteObservation compares a fully verified observation to known state.
// It fails closed on every validation or exact scope mismatch and does not
// mutate either input. A valid, verified empty observation removes every known
// item in its exact scope.
func DiffCompleteObservation(observation CompleteObservation, known KnownInventoryState) (ObservationDiff, error) {
	if err := ValidateCompleteObservation(observation); err != nil {
		return ObservationDiff{}, err
	}
	if err := ValidateKnownInventoryState(known); err != nil {
		return ObservationDiff{}, err
	}
	if !sameObservationScope(observation.Scope, known.Scope) {
		return ObservationDiff{}, fmt.Errorf("%w: source, configuration revision, selected libraries, or enabled media types differ", ErrObservationScopeMismatch)
	}

	knownByIdentity := make(map[SourceLocalIdentity]ObservedItem, len(known.Items))
	for _, item := range known.Items {
		knownByIdentity[known.Scope.Identity(item)] = item
	}
	seen := make(map[SourceLocalIdentity]struct{}, len(observation.Items))
	var diff ObservationDiff
	for _, item := range observation.Items {
		identity := observation.Scope.Identity(item)
		seen[identity] = struct{}{}
		prior, exists := knownByIdentity[identity]
		if !exists {
			diff.Added = append(diff.Added, item)
		} else if itemsEqual(item, prior) {
			diff.Unchanged = append(diff.Unchanged, item)
		} else {
			diff.Updated = append(diff.Updated, item)
		}
	}
	for _, item := range known.Items {
		if _, exists := seen[known.Scope.Identity(item)]; !exists {
			diff.Removed = append(diff.Removed, item)
		}
	}
	sortObservationDiff(&diff, observation.Scope)
	return diff, nil
}

func validateObservationScope(scope ObservationScope) error {
	if !validDurableID(scope.SourceID) || scope.ConfigurationRevision <= 0 {
		return errors.New("source ID and positive configuration revision are required")
	}
	if len(scope.SelectedLibraries) == 0 || len(scope.SelectedLibraries) > maxSelectedLibraries {
		return errors.New("one or more selected libraries are required")
	}
	if len(scope.EnabledMediaTypes) == 0 || len(scope.EnabledMediaTypes) > maxConfiguredMediaTypes {
		return errors.New("one or more enabled media types are required")
	}
	libraries := make(map[string]struct{}, len(scope.SelectedLibraries))
	for _, libraryID := range scope.SelectedLibraries {
		if !validSnapshotText(libraryID, maxLibraryIDLength) || libraryID != strings.TrimSpace(libraryID) {
			return errors.New("invalid selected library")
		}
		if _, exists := libraries[libraryID]; exists {
			return errors.New("duplicate selected library")
		}
		libraries[libraryID] = struct{}{}
	}
	mediaTypes := make(map[domain.MediaType]struct{}, len(scope.EnabledMediaTypes))
	for _, mediaType := range scope.EnabledMediaTypes {
		if !mediaType.Valid() {
			return errors.New("invalid enabled media type")
		}
		if _, exists := mediaTypes[mediaType]; exists {
			return errors.New("duplicate enabled media type")
		}
		mediaTypes[mediaType] = struct{}{}
	}
	return nil
}

func validateObservationCoverage(scope ObservationScope, coverage []ObservationCoverage) error {
	want := len(scope.SelectedLibraries) * len(scope.EnabledMediaTypes)
	if len(coverage) != want {
		return errors.New("coverage does not prove every selected library and enabled media type")
	}
	selectedLibraries, enabledMediaTypes := stringSet(scope.SelectedLibraries), mediaTypeSet(scope.EnabledMediaTypes)
	seen := make(map[coveragePair]struct{}, len(coverage))
	for _, entry := range coverage {
		if !validSnapshotText(entry.RemoteLibraryID, maxLibraryIDLength) || entry.RemoteLibraryID != strings.TrimSpace(entry.RemoteLibraryID) {
			return errors.New("invalid coverage library")
		}
		if _, ok := selectedLibraries[entry.RemoteLibraryID]; !ok {
			return errors.New("coverage library is outside selected scope")
		}
		if !entry.MediaType.Valid() {
			return errors.New("invalid coverage media type")
		}
		if _, ok := enabledMediaTypes[entry.MediaType]; !ok {
			return errors.New("coverage media type is outside enabled scope")
		}
		if entry.Status != CoverageVerifiedComplete && entry.Status != CoverageInapplicable {
			return errors.New("coverage is not complete or inapplicable")
		}
		pair := coveragePair{libraryID: entry.RemoteLibraryID, mediaType: entry.MediaType}
		if _, ok := seen[pair]; ok {
			return errors.New("duplicate coverage pair")
		}
		seen[pair] = struct{}{}
	}
	return nil
}

type coveragePair struct {
	libraryID string
	mediaType domain.MediaType
}

func validateObservationItems(scope ObservationScope, items []ObservedItem) error {
	if len(items) > maxObservationItems {
		return errors.New("too many items")
	}
	libraries := stringSet(scope.SelectedLibraries)
	mediaTypes := mediaTypeSet(scope.EnabledMediaTypes)
	identities := make(map[SourceLocalIdentity]struct{}, len(items))
	for _, item := range items {
		libraryID, itemID := item.RemoteLibraryID, item.ItemID
		if !validSnapshotText(libraryID, maxLibraryIDLength) || !validSnapshotText(itemID, maxItemIDLength) ||
			!item.MediaType.Valid() || !validSnapshotText(item.Title, maxTitleLength) || !validSnapshotText(item.Revision, maxItemRevisionSize) || len(item.ExternalURL) > maxItemURLSize ||
			libraryID != strings.TrimSpace(libraryID) || itemID != strings.TrimSpace(itemID) || item.Title != strings.TrimSpace(item.Title) || item.Revision != strings.TrimSpace(item.Revision) {
			return errors.New("invalid item")
		}
		if _, selected := libraries[libraryID]; !selected {
			return errors.New("item library is outside selected scope")
		}
		if _, enabled := mediaTypes[item.MediaType]; !enabled {
			return errors.New("item media type is outside enabled scope")
		}
		canonicalURL, err := NormalizeExternalURL(item.ExternalURL)
		if err != nil {
			return fmt.Errorf("invalid item external URL: %w", err)
		}
		if item.ExternalURL != canonicalURL {
			return errors.New("item external URL is not canonical")
		}
		identity := scope.Identity(item)
		if _, exists := identities[identity]; exists {
			return errors.New("duplicate source-local item")
		}
		identities[identity] = struct{}{}
	}
	return nil
}

func validateItemsCovered(items []ObservedItem, coverage []ObservationCoverage) error {
	applicable := make(map[coveragePair]CoverageStatus, len(coverage))
	for _, entry := range coverage {
		applicable[coveragePair{libraryID: entry.RemoteLibraryID, mediaType: entry.MediaType}] = entry.Status
	}
	for _, item := range items {
		if applicable[coveragePair{libraryID: item.RemoteLibraryID, mediaType: item.MediaType}] != CoverageVerifiedComplete {
			return errors.New("item has no verified complete coverage")
		}
	}
	return nil
}

func sameObservationScope(left, right ObservationScope) bool {
	return left.SourceID == right.SourceID &&
		left.ConfigurationRevision == right.ConfigurationRevision &&
		setsEqual(stringSet(left.SelectedLibraries), stringSet(right.SelectedLibraries)) &&
		setsEqual(mediaTypeSet(left.EnabledMediaTypes), mediaTypeSet(right.EnabledMediaTypes))
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[strings.TrimSpace(value)] = struct{}{}
	}
	return set
}

func mediaTypeSet(values []domain.MediaType) map[domain.MediaType]struct{} {
	set := make(map[domain.MediaType]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func setsEqual[T comparable](left, right map[T]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for value := range left {
		if _, exists := right[value]; !exists {
			return false
		}
	}
	return true
}

func itemsEqual(left, right ObservedItem) bool {
	return left.MediaType == right.MediaType && left.Title == right.Title &&
		left.ExternalURL == right.ExternalURL && left.Revision == right.Revision
}

func sortObservationDiff(diff *ObservationDiff, scope ObservationScope) {
	sortItems := func(items []ObservedItem) {
		sort.Slice(items, func(i, j int) bool {
			left, right := scope.Identity(items[i]), scope.Identity(items[j])
			if left.SourceID != right.SourceID {
				return left.SourceID < right.SourceID
			}
			if left.RemoteLibraryID != right.RemoteLibraryID {
				return left.RemoteLibraryID < right.RemoteLibraryID
			}
			return left.ItemID < right.ItemID
		})
	}
	sortItems(diff.Unchanged)
	sortItems(diff.Added)
	sortItems(diff.Updated)
	sortItems(diff.Removed)
}
