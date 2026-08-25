package inventory

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/secrets"
)

var (
	ErrInvalidSourceConfiguration   = errors.New("invalid inventory source configuration")
	ErrInvalidAuthoritativeCycle    = errors.New("invalid authoritative inventory cycle")
	ErrInvalidSourceSave            = errors.New("invalid inventory source save")
	ErrSourceRevisionConflict       = errors.New("inventory source revision conflict")
	ErrSourceSaveReplayConflict     = errors.New("inventory source save replay conflict")
	ErrSourceReconfigurationBusy    = errors.New("inventory source reconfiguration blocked while sync is running")
	ErrInvalidManualSyncRequest     = errors.New("invalid inventory manual sync request")
	ErrManualSyncRequestConflict    = errors.New("inventory manual sync request conflict")
	ErrManualSyncSourceNotFound     = errors.New("inventory manual sync source not found")
	ErrManualSyncSourceUnconfigured = errors.New("inventory manual sync source is unconfigured")
	ErrManualSyncSourceWrongKind    = errors.New("inventory manual sync source has unsupported kind")
	ErrManualSyncSourceDisabled     = errors.New("inventory manual sync source is disabled")
	// Connection probe errors are deliberately stable and secret-free so a
	// future HTTP boundary can map them without exposing remote details.
	ErrProbeInvalidDestination   = errors.New("inventory connection probe invalid destination")
	ErrProbeAuthenticationFailed = errors.New("inventory connection probe authentication failed")
	ErrProbeConnectionFailed     = errors.New("inventory connection probe connection failed")
	ErrProbeResponseInvalid      = errors.New("inventory connection probe response invalid")
	ErrProbeTesterUnavailable    = errors.New("inventory connection probe tester unavailable")
)

// ConnectionProbe performs a non-persisting candidate-connection check. It
// must not load source configuration, write credentials, or mutate inventory
// state. Results are deliberately limited to remote library identities.
type ConnectionProbe interface {
	Probe(context.Context, ConnectionProbeRequest) (ConnectionProbeResult, error)
}

// ConnectionProbeRequest contains only the candidate connection material
// supplied at the boundary. Credential is write-only and must never be
// returned, persisted, or included in errors.
type ConnectionProbeRequest struct {
	EndpointURL string
	Credential  string
}

// ConnectionProbeResult is the bounded safe output from a successful probe.
type ConnectionProbeResult struct {
	Libraries []RemoteLibrary
}

const (
	InventoryCredentialNamespace = "inventory-source"
	InventoryCredentialName      = "credential"
	// SourceEndpointURLBytesMax matches the durable endpoint_url constraint.
	// Keep this at the inventory boundary so invalid commands do not surface as
	// database availability failures.
	SourceEndpointURLBytesMax = 2048
)

// SourceSave is the atomic source-configuration write contract. Credential is
// already encrypted and is never returned by this package or its repositories.
type SourceSave struct {
	Configuration SourceConfiguration
	EndpointURL   string
	Credential    secrets.Envelope
	// ExpectedRevision is a compare-and-swap precondition. Zero creates a new
	// source; a non-zero value updates exactly that persisted revision.
	ExpectedRevision int64
	ActorID          string
	AuditID          string
	OccurredAt       time.Time
}

// SourceSaveResult identifies the committed immutable configuration revision.
// Replaying an already-recorded audit ID returns the revision from that save.
type SourceSaveResult struct {
	Revision      int64
	AuditReplayed bool
}

// SourceSaveRepository commits source metadata, selected libraries, encrypted
// envelope, idle status, and audit evidence in one transaction.
type SourceSaveRepository interface {
	SaveSource(context.Context, SourceSave) (SourceSaveResult, error)
}

// SourceConfigurationService contains no reader, HTTP, or scheduler wiring.
type SourceConfigurationService struct{ repository SourceSaveRepository }

func NewSourceConfigurationService(repository SourceSaveRepository) *SourceConfigurationService {
	return &SourceConfigurationService{repository: repository}
}

func (s *SourceConfigurationService) Save(ctx context.Context, save SourceSave) (SourceSaveResult, error) {
	if err := ValidateSourceSave(save); err != nil {
		return SourceSaveResult{}, err
	}
	if s == nil || s.repository == nil {
		return SourceSaveResult{}, fmt.Errorf("%w: repository is required", ErrInvalidSourceSave)
	}
	return s.repository.SaveSource(ctx, save)
}

func ValidateSourceSave(save SourceSave) error {
	configuration := CanonicalSourceConfiguration(save.Configuration)
	if err := ValidateSourceConfiguration(configuration); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSourceSave, err)
	}
	if !configuration.Enabled || len(configuration.SelectedLibraries) == 0 {
		return fmt.Errorf("%w: enabled source and selected libraries are required", ErrInvalidSourceSave)
	}
	if !configuredSourceKind(configuration.Kind) {
		return fmt.Errorf("%w: only Plex and Audiobookshelf sources can be configured", ErrInvalidSourceSave)
	}
	if !sourceKindAllowsMediaTypes(configuration.Kind, configuration.MediaTypes) {
		return fmt.Errorf("%w: source media types are not supported by its reader", ErrInvalidSourceSave)
	}
	if save.ExpectedRevision < 0 {
		return fmt.Errorf("%w: expected revision is invalid", ErrInvalidSourceSave)
	}
	endpoint, err := NormalizeExternalURL(save.EndpointURL)
	if err != nil || endpoint == "" {
		return fmt.Errorf("%w: endpoint URL is required", ErrInvalidSourceSave)
	}
	if len(endpoint) > SourceEndpointURLBytesMax {
		return fmt.Errorf("%w: endpoint URL exceeds %d bytes", ErrInvalidSourceSave, SourceEndpointURLBytesMax)
	}
	if !validDurableID(save.AuditID) || save.OccurredAt.IsZero() || (save.ActorID != "" && !validDurableID(save.ActorID)) {
		return fmt.Errorf("%w: audit ID, timestamp, and actor are invalid", ErrInvalidSourceSave)
	}
	if save.Credential.Version != secrets.EnvelopeVersion || len(save.Credential.Ciphertext) < 16 || len(save.Credential.Ciphertext) > secrets.MaxCiphertext || save.Credential.Identity == ([32]byte{}) {
		return fmt.Errorf("%w: encrypted credential envelope is invalid", ErrInvalidSourceSave)
	}
	return nil
}

func configuredSourceKind(kind SourceKind) bool {
	return kind == SourcePlex || kind == SourceAudiobookshelf
}

func sourceKindAllowsMediaTypes(kind SourceKind, mediaTypes []domain.MediaType) bool {
	for _, mediaType := range mediaTypes {
		switch kind {
		case SourcePlex:
			if mediaType != domain.MediaMovie && mediaType != domain.MediaTV && mediaType != domain.MediaAlbum && mediaType != domain.MediaSong {
				return false
			}
		case SourceAudiobookshelf:
			if mediaType != domain.MediaAudiobook {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ManualSyncRequest records an operator's request for a later source sync. It
// has no source configuration, credential, adapter, or execution fields.
// ID, RequestedBy, and RequestedAt are trusted server-side inputs. An
// authenticated boundary must derive them rather than accepting them from an
// untrusted client request.
type ManualSyncRequest struct {
	ID          string
	SourceID    string
	RequestedBy string
	RequestedAt time.Time
}

type ManualSyncDisposition string

const (
	ManualSyncRecorded  ManualSyncDisposition = "recorded"
	ManualSyncReplayed  ManualSyncDisposition = "replayed"
	ManualSyncCoalesced ManualSyncDisposition = "coalesced"
)

// ManualSyncRequestResult describes the durable request operation. PendingIntentID
// identifies the pending work record and is returned for every successful
// disposition: recorded, coalesced, and replayed.
type ManualSyncRequestResult struct {
	Disposition     ManualSyncDisposition
	PendingIntentID string
}

type ManualSyncRequestRepository interface {
	RequestManualSync(context.Context, ManualSyncRequest) (ManualSyncRequestResult, error)
}

type ManualSyncService struct{ repository ManualSyncRequestRepository }

func NewManualSyncService(repository ManualSyncRequestRepository) *ManualSyncService {
	return &ManualSyncService{repository: repository}
}

func (s *ManualSyncService) Request(ctx context.Context, request ManualSyncRequest) (ManualSyncRequestResult, error) {
	if err := ValidateManualSyncRequest(request); err != nil {
		return ManualSyncRequestResult{}, err
	}
	if s == nil || s.repository == nil {
		return ManualSyncRequestResult{}, fmt.Errorf("%w: repository is required", ErrInvalidManualSyncRequest)
	}
	return s.repository.RequestManualSync(ctx, CanonicalManualSyncRequest(request))
}

func ValidateManualSyncRequest(request ManualSyncRequest) error {
	if !validDurableID(strings.TrimSpace(request.ID)) || !validDurableID(strings.TrimSpace(request.SourceID)) ||
		(request.RequestedBy != "" && !validDurableID(strings.TrimSpace(request.RequestedBy))) || request.RequestedAt.IsZero() {
		return fmt.Errorf("%w: request ID, source, actor, and timestamp are invalid", ErrInvalidManualSyncRequest)
	}
	return nil
}

func CanonicalManualSyncRequest(request ManualSyncRequest) ManualSyncRequest {
	request.ID = strings.TrimSpace(request.ID)
	request.SourceID = strings.TrimSpace(request.SourceID)
	request.RequestedBy = strings.TrimSpace(request.RequestedBy)
	request.RequestedAt = request.RequestedAt.UTC()
	return request
}

const (
	maxConfiguredMediaTypes = 6
	maxConfiguredLibraries  = 100
	maxCycleEvents          = 10_000
)

var durableIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// validDurableID accepts the canonical textual UUID representation supported
// by the durable database columns. Source-native IDs remain opaque strings.
func validDurableID(value string) bool {
	return durableIDPattern.MatchString(value)
}

// SourceKind identifies a library product without coupling inventory policy to
// a concrete client implementation.
type SourceKind string

const (
	SourcePlex           SourceKind = "plex"
	SourceJellyfin       SourceKind = "jellyfin"
	SourceNavidrome      SourceKind = "navidrome"
	SourceRomM           SourceKind = "romm"
	SourceAudiobookshelf SourceKind = "audiobookshelf"
)

func (k SourceKind) Valid() bool {
	switch k {
	case SourcePlex, SourceJellyfin, SourceNavidrome, SourceRomM, SourceAudiobookshelf:
		return true
	default:
		return false
	}
}

// SourceCapabilities describe what an adapter can safely provide. A source
// must support complete enumeration before it can be authoritative.
type SourceCapabilities struct {
	CompleteEnumeration bool
	Delta               bool
}

// SourceConfiguration is secret-free durable configuration. Endpoint URLs and
// credentials deliberately belong to adapter-specific encrypted settings.
type SourceConfiguration struct {
	ID                string
	Name              string
	Kind              SourceKind
	Enabled           bool
	MediaTypes        []domain.MediaType
	SelectedLibraries []string
	Capabilities      SourceCapabilities
}

func ValidateSourceConfiguration(configuration SourceConfiguration) error {
	if !validDurableID(configuration.ID) || !validSnapshotText(configuration.Name, maxLibraryNameLength) || !configuration.Kind.Valid() {
		return fmt.Errorf("%w: ID, name, and kind are required", ErrInvalidSourceConfiguration)
	}
	if !configuration.Capabilities.CompleteEnumeration {
		return fmt.Errorf("%w: complete enumeration capability is required", ErrInvalidSourceConfiguration)
	}
	if len(configuration.MediaTypes) == 0 || len(configuration.MediaTypes) > maxConfiguredMediaTypes {
		return fmt.Errorf("%w: one or more media types are required", ErrInvalidSourceConfiguration)
	}
	mediaTypes := make(map[domain.MediaType]struct{}, len(configuration.MediaTypes))
	for _, mediaType := range configuration.MediaTypes {
		if !mediaType.Valid() {
			return fmt.Errorf("%w: unsupported media type", ErrInvalidSourceConfiguration)
		}
		if _, exists := mediaTypes[mediaType]; exists {
			return fmt.Errorf("%w: duplicate media type", ErrInvalidSourceConfiguration)
		}
		mediaTypes[mediaType] = struct{}{}
	}
	if len(configuration.SelectedLibraries) > maxConfiguredLibraries {
		return fmt.Errorf("%w: too many selected libraries", ErrInvalidSourceConfiguration)
	}
	libraries := make(map[string]struct{}, len(configuration.SelectedLibraries))
	for _, libraryID := range configuration.SelectedLibraries {
		if !validSnapshotText(libraryID, maxLibraryIDLength) {
			return fmt.Errorf("%w: invalid selected library", ErrInvalidSourceConfiguration)
		}
		libraryID = strings.TrimSpace(libraryID)
		if _, exists := libraries[libraryID]; exists {
			return fmt.Errorf("%w: duplicate selected library", ErrInvalidSourceConfiguration)
		}
		libraries[libraryID] = struct{}{}
	}
	return nil
}

// CanonicalSourceConfiguration returns a deterministic copy for idempotency
// fingerprints without mutating caller-owned slices.
func CanonicalSourceConfiguration(configuration SourceConfiguration) SourceConfiguration {
	copy := configuration
	copy.ID = strings.TrimSpace(copy.ID)
	copy.Name = strings.TrimSpace(copy.Name)
	copy.MediaTypes = append([]domain.MediaType(nil), configuration.MediaTypes...)
	copy.SelectedLibraries = append([]string(nil), configuration.SelectedLibraries...)
	for i := range copy.SelectedLibraries {
		copy.SelectedLibraries[i] = strings.TrimSpace(copy.SelectedLibraries[i])
	}
	sort.Slice(copy.MediaTypes, func(i, j int) bool { return copy.MediaTypes[i] < copy.MediaTypes[j] })
	sort.Strings(copy.SelectedLibraries)
	return copy
}

type ChangeKind string

const (
	ChangeAdded   ChangeKind = "added"
	ChangeUpdated ChangeKind = "updated"
	ChangeRemoved ChangeKind = "removed"
)

func (k ChangeKind) Valid() bool {
	return k == ChangeAdded || k == ChangeUpdated || k == ChangeRemoved
}

// InventoryEvent is an audit-only, source-local change emitted only by a
// committed complete cycle. It is not an availability decision and must not be
// projected into normalized inventory items: it has no bounded, normalized item
// fields or matching evidence. A future item projection needs its own complete
// validation and authoritative availability rules.
type InventoryEvent struct {
	Kind         ChangeKind
	SourceItemID string
	MediaType    domain.MediaType
}

// AuthoritativeCycle is the commit boundary that prevents partial source data
// from removing last-known inventory.
type AuthoritativeCycle struct {
	ID          string
	SourceID    string
	StartedAt   time.Time
	CompletedAt time.Time
	Complete    bool
	Events      []InventoryEvent
}

type CycleCommit struct {
	CycleID   string
	Committed bool
}

// AuthoritativeCycleRepository persists a complete inventory decision and its
// events atomically. Failed or incomplete cycles must never be passed here.
type AuthoritativeCycleRepository interface {
	CommitAuthoritativeCycle(context.Context, AuthoritativeCycle) (CycleCommit, error)
}

func ValidateAuthoritativeCycle(cycle AuthoritativeCycle) error {
	if !cycle.Complete {
		return fmt.Errorf("%w: cycle is incomplete", ErrInvalidAuthoritativeCycle)
	}
	if !validDurableID(cycle.ID) || !validDurableID(cycle.SourceID) || cycle.StartedAt.IsZero() || cycle.CompletedAt.IsZero() || cycle.CompletedAt.Before(cycle.StartedAt) {
		return fmt.Errorf("%w: ID, source, and ordered timestamps are required", ErrInvalidAuthoritativeCycle)
	}
	if len(cycle.Events) > maxCycleEvents {
		return fmt.Errorf("%w: too many events", ErrInvalidAuthoritativeCycle)
	}
	events := make(map[string]struct{}, len(cycle.Events))
	for _, event := range cycle.Events {
		if !event.Kind.Valid() || !event.MediaType.Valid() || !validSnapshotText(event.SourceItemID, maxItemIDLength) {
			return fmt.Errorf("%w: invalid event", ErrInvalidAuthoritativeCycle)
		}
		// A cycle is a state transition, not an event log. Multiple changes for
		// one source item would make its persisted result order-dependent.
		key := strings.TrimSpace(event.SourceItemID)
		if _, exists := events[key]; exists {
			return fmt.Errorf("%w: duplicate event", ErrInvalidAuthoritativeCycle)
		}
		events[key] = struct{}{}
	}
	return nil
}

type SourceSyncState string

const (
	SourceSyncIdle      SourceSyncState = "idle"
	SourceSyncRunning   SourceSyncState = "running"
	SourceSyncSucceeded SourceSyncState = "succeeded"
	SourceSyncFailed    SourceSyncState = "failed"
)

func (s SourceSyncState) Valid() bool {
	return s == SourceSyncIdle || s == SourceSyncRunning || s == SourceSyncSucceeded || s == SourceSyncFailed
}

// SourceFailureDetail is a stable, secret-free diagnosis appropriate for
// durable status and UI display. Adapter, HTTP, and database errors must be
// mapped to one of these values before crossing the inventory boundary.
type SourceFailureDetail string

const (
	SourceFailureAuthenticationFailed SourceFailureDetail = "authentication failed"
	SourceFailureRequestTimedOut      SourceFailureDetail = "request timed out"
	SourceFailureUnavailable          SourceFailureDetail = "source unavailable"
	SourceFailureInvalidResponse      SourceFailureDetail = "invalid source response"
)

func (d SourceFailureDetail) Valid() bool {
	switch d {
	case SourceFailureAuthenticationFailed, SourceFailureRequestTimedOut, SourceFailureUnavailable, SourceFailureInvalidResponse:
		return true
	default:
		return false
	}
}

// SourceStatus is local operational state. FailureDetail must be a concise,
// secret-free diagnostic selected by the caller, never an adapter response.
type SourceStatus struct {
	SourceID         string
	State            SourceSyncState
	LastStartedAt    time.Time
	LastCompletedAt  time.Time
	LastSuccessfulAt time.Time
	FailureDetail    string
}

// SourceStatusTransition is the only supported durable source-status mutation.
// CycleID is an idempotency key: persisting the same transition again must
// return the original result without changing timestamps or status.
type SourceStatusTransition struct {
	SourceID      string
	CycleID       string
	From          SourceSyncState
	To            SourceSyncState
	OccurredAt    time.Time
	FailureDetail string
}

// SourceStatusTransitionResult reports whether a transition was newly applied.
// Current is returned for both new and repeated transition requests.
type SourceStatusTransitionResult struct {
	Applied bool
	Current SourceStatus
}

// SourceStatusRepository persists compare-and-transition operations atomically.
// Implementations must reject a stale From state and deduplicate by SourceID,
// CycleID, and transition content.
type SourceStatusRepository interface {
	TransitionSourceStatus(context.Context, SourceStatusTransition) (SourceStatusTransitionResult, error)
}

func ValidateSourceStatusTransition(transition SourceStatusTransition) error {
	if !validDurableID(transition.SourceID) || !validDurableID(transition.CycleID) || !transition.From.Valid() || !transition.To.Valid() || transition.OccurredAt.IsZero() {
		return fmt.Errorf("%w: invalid source status transition", ErrInvalidAuthoritativeCycle)
	}
	if !validSourceStatusTransition(transition.From, transition.To) {
		return fmt.Errorf("%w: unsupported source status transition", ErrInvalidAuthoritativeCycle)
	}
	failure := strings.TrimSpace(transition.FailureDetail)
	if (transition.To == SourceSyncFailed && !SourceFailureDetail(failure).Valid()) || (transition.To != SourceSyncFailed && failure != "") {
		return fmt.Errorf("%w: invalid transition failure detail", ErrInvalidAuthoritativeCycle)
	}
	return nil
}

func validSourceStatusTransition(from, to SourceSyncState) bool {
	if from == to {
		return true
	}
	if to == SourceSyncRunning {
		return from == SourceSyncIdle || from == SourceSyncSucceeded || from == SourceSyncFailed
	}
	return from == SourceSyncRunning && (to == SourceSyncSucceeded || to == SourceSyncFailed)
}

func ValidateSourceStatus(status SourceStatus) error {
	if !validDurableID(status.SourceID) || !status.State.Valid() {
		return fmt.Errorf("%w: invalid source status", ErrInvalidAuthoritativeCycle)
	}
	if status.FailureDetail != "" && !SourceFailureDetail(status.FailureDetail).Valid() {
		return fmt.Errorf("%w: invalid failure detail", ErrInvalidAuthoritativeCycle)
	}
	if !status.LastCompletedAt.IsZero() && !status.LastStartedAt.IsZero() && status.LastCompletedAt.Before(status.LastStartedAt) {
		return fmt.Errorf("%w: completion precedes start", ErrInvalidAuthoritativeCycle)
	}
	if !status.LastSuccessfulAt.IsZero() && (!status.LastCompletedAt.IsZero() && status.LastSuccessfulAt.After(status.LastCompletedAt)) {
		return fmt.Errorf("%w: success follows completion", ErrInvalidAuthoritativeCycle)
	}
	if status.State == SourceSyncFailed && !SourceFailureDetail(status.FailureDetail).Valid() {
		return fmt.Errorf("%w: failed status requires failure detail", ErrInvalidAuthoritativeCycle)
	}
	if status.State != SourceSyncFailed && strings.TrimSpace(status.FailureDetail) != "" {
		return fmt.Errorf("%w: only failed status may include failure detail", ErrInvalidAuthoritativeCycle)
	}
	return nil
}

func utf8ValidStatusText(value string) bool {
	return validSnapshotText(value, maxTitleLength) || value == ""
}
