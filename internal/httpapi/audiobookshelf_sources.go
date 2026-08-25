package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
	"git.example.com/evilperson1337/Requestarr/internal/secrets"
	"git.example.com/evilperson1337/Requestarr/internal/store/postgres"
)

const sourceRequestBodyMax = int64(32 * 1024)

// AudiobookshelfSourceOptions contains only the narrow operations the admin
// HTTP surface needs. Query operations use local persisted projections only.
type AudiobookshelfSourceOptions struct {
	Query      inventory.SourceConfigurationQueryRepository
	Save       inventory.SourceConfigurationSaveRepository
	ManualSync interface {
		Request(context.Context, inventory.ManualSyncRequest) (inventory.ManualSyncRequestResult, error)
	}
	Probe  inventory.ConnectionProbe
	Cipher *secrets.Cipher
	Auth   AuthOptions
	// NewID derives server-controlled UUIDs. It must use cryptographic
	// randomness and returns an error rather than falling back to a known ID.
	NewID func() (string, error)
}

type sourceConfigurationRequest struct {
	Name              string   `json:"name"`
	EndpointURL       string   `json:"endpointURL"`
	Enabled           bool     `json:"enabled"`
	SelectedLibraries []string `json:"selectedLibraries"`
	Credential        string   `json:"credential"`
	CredentialMode    string   `json:"credentialMode"`
	ExpectedRevision  *int64   `json:"expectedRevision"`
}

type sourceProbeRequest struct {
	EndpointURL string `json:"endpointURL"`
	Credential  string `json:"credential"`
}

type sourceResponse struct {
	ID                   string         `json:"id"`
	Name                 string         `json:"name"`
	Kind                 string         `json:"kind"`
	EndpointURL          string         `json:"endpointURL"`
	Enabled              bool           `json:"enabled"`
	MediaTypes           []string       `json:"mediaTypes"`
	SelectedLibraries    []string       `json:"selectedLibraries"`
	Revision             int64          `json:"revision"`
	CredentialConfigured bool           `json:"credentialConfigured"`
	Status               statusResponse `json:"status"`
}

type statusResponse struct {
	State            string     `json:"state"`
	LastStartedAt    *time.Time `json:"lastStartedAt,omitempty"`
	LastCompletedAt  *time.Time `json:"lastCompletedAt,omitempty"`
	LastSuccessfulAt *time.Time `json:"lastSuccessfulAt,omitempty"`
	FailureDetail    string     `json:"failureDetail,omitempty"`
}

func audiobookshelfSourceRoutes(mux *http.ServeMux, options AudiobookshelfSourceOptions) {
	wrap := func(next http.Handler) http.Handler { return noStore(requireAdmin(options.Auth, next)) }
	mux.Handle("/api/v1/admin/inventory/audiobookshelf/sources", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listSources(w, r, options)
		case http.MethodPost:
			saveSource(w, r, options, "")
		default:
			writeMethodNotAllowed(w, r, http.MethodGet, http.MethodPost)
		}
	})))
	mux.Handle("/api/v1/admin/inventory/audiobookshelf/sources/connection-test", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r, http.MethodPost)
			return
		}
		probeSource(w, r, options)
	})))
	mux.Handle("/api/v1/admin/inventory/audiobookshelf/sources/{sourceID}", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceID := r.PathValue("sourceID")
		if !validSourceID(w, r, sourceID) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			getSource(w, r, options, sourceID)
		case http.MethodPut:
			saveSource(w, r, options, sourceID)
		default:
			writeMethodNotAllowed(w, r, http.MethodGet, http.MethodPut)
		}
	})))
	mux.Handle("/api/v1/admin/inventory/audiobookshelf/sources/{sourceID}/sync", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r, http.MethodPost)
			return
		}
		sourceID := r.PathValue("sourceID")
		if !validSourceID(w, r, sourceID) {
			return
		}
		requestSourceSync(w, r, options, sourceID)
	})))
}

func listSources(w http.ResponseWriter, r *http.Request, options AudiobookshelfSourceOptions) {
	if options.Query == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "source_configuration_unavailable", "Source configuration is temporarily unavailable.")
		return
	}
	views, err := options.Query.ListSourceConfigurations(r.Context())
	if err != nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "source_configuration_unavailable", "Source configuration is temporarily unavailable.")
		return
	}
	result := make([]sourceResponse, 0, len(views))
	for _, view := range views {
		result = append(result, sourceViewResponse(view))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": result})
}

func getSource(w http.ResponseWriter, r *http.Request, options AudiobookshelfSourceOptions, sourceID string) {
	if options.Query == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "source_configuration_unavailable", "Source configuration is temporarily unavailable.")
		return
	}
	view, err := options.Query.GetSourceConfiguration(r.Context(), sourceID)
	if errors.Is(err, inventory.ErrSourceNotFound) {
		writeAPIError(w, r, http.StatusNotFound, "source_not_found", "The Audiobookshelf source was not found.")
		return
	}
	if err != nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "source_configuration_unavailable", "Source configuration is temporarily unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, sourceViewResponse(view))
}

func saveSource(w http.ResponseWriter, r *http.Request, options AudiobookshelfSourceOptions, pathID string) {
	if !validSourceMutation(w, r, options.Auth.PublicOrigin) {
		return
	}
	var request sourceConfigurationRequest
	if err := decodeJSON(w, r, &request, sourceRequestBodyMax, true); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
		return
	}
	create := pathID == ""
	if request.ExpectedRevision == nil ||
		(create && (request.CredentialMode != "replace" || request.Credential == "" || *request.ExpectedRevision != 0)) ||
		(!create && (request.CredentialMode != "preserve" && request.CredentialMode != "replace" || *request.ExpectedRevision < 1 || (request.CredentialMode == "replace" && request.Credential == "") || (request.CredentialMode == "preserve" && request.Credential != ""))) {
		writeAPIError(w, r, http.StatusUnprocessableEntity, "source_configuration_invalid", "The Audiobookshelf source configuration is invalid.")
		return
	}
	if options.Save == nil || options.Cipher == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "source_configuration_unavailable", "Source configuration is temporarily unavailable.")
		return
	}
	sourceID := pathID
	if create {
		var err error
		sourceID, err = sourceIDGenerator(options)()
		if err != nil {
			writeAPIError(w, r, http.StatusServiceUnavailable, "source_configuration_unavailable", "Source configuration is temporarily unavailable.")
			return
		}
	}
	auditID, err := sourceIDGenerator(options)()
	if err != nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "source_configuration_unavailable", "Source configuration is temporarily unavailable.")
		return
	}
	credential := inventory.SourceCredentialChange{Preserve: request.CredentialMode == "preserve"}
	if !credential.Preserve {
		owner, ok := sourceOwnerID(sourceID)
		if !ok {
			writeAPIError(w, r, http.StatusUnprocessableEntity, "source_configuration_invalid", "The Audiobookshelf source configuration is invalid.")
			return
		}
		envelope, err := options.Cipher.Encrypt(secrets.Context{Namespace: inventory.InventoryCredentialNamespace, OwnerID: owner, Name: inventory.InventoryCredentialName}, []byte(request.Credential))
		if err != nil {
			writeAPIError(w, r, http.StatusServiceUnavailable, "source_configuration_unavailable", "Source configuration is temporarily unavailable.")
			return
		}
		credential.Replace = envelope
	}
	principal := readAuthSession(r, options.Auth).principal
	save := inventory.SourceConfigurationSave{Configuration: inventory.SourceConfiguration{ID: sourceID, Name: request.Name, Kind: inventory.SourceAudiobookshelf, Enabled: request.Enabled, MediaTypes: []domain.MediaType{domain.MediaAudiobook}, SelectedLibraries: request.SelectedLibraries, Capabilities: inventory.SourceCapabilities{CompleteEnumeration: true}}, EndpointURL: request.EndpointURL, Credential: credential, ExpectedRevision: *request.ExpectedRevision, ActorID: principal.UserID, AuditID: auditID, OccurredAt: time.Now().UTC()}
	result, err := options.Save.SaveSourceConfiguration(r.Context(), save)
	if err != nil {
		writeSourceSaveError(w, r, err)
		return
	}
	writeJSON(w, map[bool]int{true: http.StatusCreated, false: http.StatusOK}[create], map[string]any{"id": sourceID, "revision": result.Revision})
}

func probeSource(w http.ResponseWriter, r *http.Request, options AudiobookshelfSourceOptions) {
	if !validSourceMutation(w, r, options.Auth.PublicOrigin) {
		return
	}
	var request sourceProbeRequest
	if err := decodeJSON(w, r, &request, sourceRequestBodyMax, true); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
		return
	}
	if options.Probe == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "connection_test_unavailable", "The connection test is temporarily unavailable.")
		return
	}
	result, err := options.Probe.Probe(r.Context(), inventory.ConnectionProbeRequest{EndpointURL: request.EndpointURL, Credential: request.Credential})
	if err != nil {
		writeProbeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"libraries": result.Libraries})
}

func requestSourceSync(w http.ResponseWriter, r *http.Request, options AudiobookshelfSourceOptions, sourceID string) {
	if !validSourceMutation(w, r, options.Auth.PublicOrigin) {
		return
	}
	var body emptyJSONObject
	if err := decodeJSON(w, r, &body, 16, true); err != nil {
		writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
		return
	}
	if options.ManualSync == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "manual_sync_unavailable", "Manual sync is temporarily unavailable.")
		return
	}
	principal := readAuthSession(r, options.Auth).principal
	requestID, err := sourceIDGenerator(options)()
	if err != nil {
		writeAPIError(w, r, http.StatusServiceUnavailable, "manual_sync_unavailable", "Manual sync is temporarily unavailable.")
		return
	}
	result, err := options.ManualSync.Request(r.Context(), inventory.ManualSyncRequest{ID: requestID, SourceID: sourceID, RequestedBy: principal.UserID, RequestedAt: time.Now().UTC()})
	if err != nil {
		writeManualSyncError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"disposition": result.Disposition, "pendingIntentID": result.PendingIntentID})
}

func validSourceMutation(w http.ResponseWriter, r *http.Request, publicOrigin string) bool {
	if !exactSetupOrigin(r, publicOrigin) {
		writeAPIError(w, r, http.StatusForbidden, "origin_forbidden", "The request origin is not allowed.")
		return false
	}
	csrf := r.Header.Values("X-Requestarr-CSRF")
	if len(csrf) != 1 || csrf[0] != "1" {
		writeAPIError(w, r, http.StatusForbidden, "csrf_forbidden", "The CSRF check failed.")
		return false
	}
	return true
}

func sourceViewResponse(view inventory.SourceConfigurationView) sourceResponse {
	mediaTypes := make([]string, len(view.Configuration.MediaTypes))
	for i, mediaType := range view.Configuration.MediaTypes {
		mediaTypes[i] = string(mediaType)
	}
	status := statusResponse{State: string(view.Status.State), FailureDetail: view.Status.FailureDetail}
	if !view.Status.LastStartedAt.IsZero() {
		value := view.Status.LastStartedAt.UTC()
		status.LastStartedAt = &value
	}
	if !view.Status.LastCompletedAt.IsZero() {
		value := view.Status.LastCompletedAt.UTC()
		status.LastCompletedAt = &value
	}
	if !view.Status.LastSuccessfulAt.IsZero() {
		value := view.Status.LastSuccessfulAt.UTC()
		status.LastSuccessfulAt = &value
	}
	return sourceResponse{ID: view.Configuration.ID, Name: view.Configuration.Name, Kind: string(view.Configuration.Kind), EndpointURL: view.EndpointURL, Enabled: view.Configuration.Enabled, MediaTypes: mediaTypes, SelectedLibraries: append([]string(nil), view.Configuration.SelectedLibraries...), Revision: view.Revision, CredentialConfigured: view.CredentialConfigured, Status: status}
}

func sourceOwnerID(id string) (secrets.OwnerID, bool) {
	var owner secrets.OwnerID
	if !isUUID(id) {
		return owner, false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	if err != nil || len(decoded) != len(owner) {
		return owner, false
	}
	copy(owner[:], decoded)
	return owner, true
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("read UUID randomness: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func sourceIDGenerator(options AudiobookshelfSourceOptions) func() (string, error) {
	if options.NewID != nil {
		return options.NewID
	}
	return newUUID
}

func validSourceID(w http.ResponseWriter, r *http.Request, sourceID string) bool {
	if isUUID(sourceID) {
		return true
	}
	writeAPIError(w, r, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
	return false
}

func isUUID(value string) bool {
	if len(value) != 36 || strings.Count(value, "-") != 4 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && len(decoded) == 16
}

type emptyJSONObject struct{}

func (emptyJSONObject) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) != "{}" {
		return errors.New("body must be an empty JSON object")
	}
	return nil
}

func writeSourceSaveError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, inventory.ErrSourceNotFound):
		writeAPIError(w, r, http.StatusNotFound, "source_not_found", "The Audiobookshelf source was not found.")
	case errors.Is(err, inventory.ErrSourceRevisionConflict), errors.Is(err, inventory.ErrSourceSaveReplayConflict), errors.Is(err, inventory.ErrSourceReconfigurationBusy):
		writeAPIError(w, r, http.StatusConflict, "source_configuration_conflict", "The Audiobookshelf source configuration could not be updated.")
	case errors.Is(err, inventory.ErrInvalidSourceSave), errors.Is(err, postgres.ErrInventoryPersistenceInvalid):
		writeAPIError(w, r, http.StatusUnprocessableEntity, "source_configuration_invalid", "The Audiobookshelf source configuration is invalid.")
	default:
		writeAPIError(w, r, http.StatusServiceUnavailable, "source_configuration_unavailable", "Source configuration is temporarily unavailable.")
	}
}

func writeProbeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, inventory.ErrProbeInvalidDestination):
		writeAPIError(w, r, http.StatusUnprocessableEntity, "connection_test_invalid_destination", "The connection-test destination is invalid.")
	case errors.Is(err, inventory.ErrProbeAuthenticationFailed):
		writeAPIError(w, r, http.StatusUnauthorized, "connection_test_authentication_failed", "The connection-test credential was rejected.")
	case errors.Is(err, inventory.ErrProbeConnectionFailed):
		writeAPIError(w, r, http.StatusBadGateway, "connection_test_connection_failed", "The connection test could not reach Audiobookshelf.")
	case errors.Is(err, inventory.ErrProbeResponseInvalid):
		writeAPIError(w, r, http.StatusBadGateway, "connection_test_response_invalid", "Audiobookshelf returned an invalid response.")
	default:
		writeAPIError(w, r, http.StatusServiceUnavailable, "connection_test_unavailable", "The connection test is temporarily unavailable.")
	}
}

func writeManualSyncError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, inventory.ErrManualSyncSourceNotFound):
		writeAPIError(w, r, http.StatusNotFound, "source_not_found", "The Audiobookshelf source was not found.")
	case errors.Is(err, inventory.ErrManualSyncSourceUnconfigured), errors.Is(err, inventory.ErrManualSyncSourceWrongKind), errors.Is(err, inventory.ErrManualSyncSourceDisabled), errors.Is(err, inventory.ErrInvalidManualSyncRequest):
		writeAPIError(w, r, http.StatusConflict, "manual_sync_unavailable", "Manual sync is unavailable for this source.")
	case errors.Is(err, inventory.ErrManualSyncRequestConflict):
		writeAPIError(w, r, http.StatusConflict, "manual_sync_conflict", "The manual sync request conflicts with an existing request.")
	default:
		writeAPIError(w, r, http.StatusServiceUnavailable, "manual_sync_unavailable", "Manual sync is temporarily unavailable.")
	}
}
