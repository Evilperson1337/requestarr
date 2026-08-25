package inventory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/secrets"
)

func TestValidateSourceConfiguration(t *testing.T) {
	valid := SourceConfiguration{ID: "05829685-9808-4a01-a0dc-744fa83a253f", Name: "Plex", Kind: SourcePlex, Enabled: true, MediaTypes: []domain.MediaType{domain.MediaMovie}, Capabilities: SourceCapabilities{CompleteEnumeration: true}}
	if err := ValidateSourceConfiguration(valid); err != nil {
		t.Fatalf("ValidateSourceConfiguration() error = %v", err)
	}
	for _, mutate := range []func(*SourceConfiguration){
		func(c *SourceConfiguration) { c.Kind = "unknown" },
		func(c *SourceConfiguration) { c.Capabilities.CompleteEnumeration = false },
		func(c *SourceConfiguration) { c.MediaTypes = []domain.MediaType{domain.MediaMovie, domain.MediaMovie} },
		func(c *SourceConfiguration) { c.SelectedLibraries = []string{"one", " one "} },
		func(c *SourceConfiguration) { c.ID = "plex-1" },
	} {
		configuration := valid
		mutate(&configuration)
		if err := ValidateSourceConfiguration(configuration); !errors.Is(err, ErrInvalidSourceConfiguration) {
			t.Fatalf("ValidateSourceConfiguration() error = %v", err)
		}
	}
}

func TestValidateSourceSave(t *testing.T) {
	valid := SourceSave{
		Configuration: SourceConfiguration{ID: "05829685-9808-4a01-a0dc-744fa83a253f", Name: "Books", Kind: SourceAudiobookshelf, Enabled: true, MediaTypes: []domain.MediaType{domain.MediaAudiobook}, SelectedLibraries: []string{"books"}, Capabilities: SourceCapabilities{CompleteEnumeration: true}},
		EndpointURL:   "https://books.example.test:13378", Credential: secrets.Envelope{Version: secrets.EnvelopeVersion, Ciphertext: make([]byte, 16), Identity: [32]byte{1}},
		AuditID: "e6e79917-23c7-4f90-98c0-bc0a70cf2d6f", OccurredAt: time.Now(),
	}
	if err := ValidateSourceSave(valid); err != nil {
		t.Fatalf("ValidateSourceSave() error = %v", err)
	}
	valid.Configuration.Enabled = false
	if err := ValidateSourceSave(valid); !errors.Is(err, ErrInvalidSourceSave) {
		t.Fatalf("ValidateSourceSave() disabled error = %v", err)
	}
	valid.Configuration.Enabled, valid.ExpectedRevision = true, -1
	if err := ValidateSourceSave(valid); !errors.Is(err, ErrInvalidSourceSave) {
		t.Fatalf("ValidateSourceSave() negative revision error = %v", err)
	}
	for _, mutate := range []func(*SourceSave){
		func(s *SourceSave) { s.EndpointURL = "" },
		func(s *SourceSave) { s.AuditID = "not-a-uuid" },
		func(s *SourceSave) { s.OccurredAt = time.Time{} },
		func(s *SourceSave) { s.Credential.Ciphertext = make([]byte, 15) },
	} {
		save := SourceSave{
			Configuration: valid.Configuration, EndpointURL: "https://books.example.test", Credential: secrets.Envelope{Version: secrets.EnvelopeVersion, Ciphertext: make([]byte, 16), Identity: [32]byte{1}},
			AuditID: "e6e79917-23c7-4f90-98c0-bc0a70cf2d6f", OccurredAt: time.Now(),
		}
		mutate(&save)
		if err := ValidateSourceSave(save); !errors.Is(err, ErrInvalidSourceSave) {
			t.Fatalf("ValidateSourceSave() expanded branch error = %v", err)
		}
	}
}

func TestValidateSourceSavePlexSemantics(t *testing.T) {
	valid := SourceSave{
		Configuration: SourceConfiguration{ID: "05829685-9808-4a01-a0dc-744fa83a253f", Name: "Plex", Kind: SourcePlex, Enabled: true, MediaTypes: []domain.MediaType{domain.MediaMovie, domain.MediaTV}, SelectedLibraries: []string{"movies"}, Capabilities: SourceCapabilities{CompleteEnumeration: true}},
		EndpointURL:   "https://plex.example.test", Credential: secrets.Envelope{Version: secrets.EnvelopeVersion, Ciphertext: make([]byte, 16), Identity: [32]byte{1}},
		AuditID: "4ecb0c32-e4b2-4b9a-ac03-337b48ea1111", OccurredAt: time.Now().UTC(),
	}
	if err := ValidateSourceSave(valid); err != nil {
		t.Fatalf("valid Plex save: %v", err)
	}
	valid.Configuration.MediaTypes = []domain.MediaType{domain.MediaAudiobook}
	if err := ValidateSourceSave(valid); !errors.Is(err, ErrInvalidSourceSave) {
		t.Fatalf("Plex audiobook save error=%v", err)
	}
	valid.Configuration.Kind = SourceJellyfin
	valid.Configuration.MediaTypes = []domain.MediaType{domain.MediaMovie}
	if err := ValidateSourceSave(valid); !errors.Is(err, ErrInvalidSourceSave) {
		t.Fatalf("unsupported configured kind error=%v", err)
	}
}

func TestValidateSourceConfigurationSaveCredentialOperation(t *testing.T) {
	valid := SourceConfigurationSave{
		Configuration: SourceConfiguration{ID: "05829685-9808-4a01-a0dc-744fa83a253f", Name: "Books", Kind: SourceAudiobookshelf, Enabled: true, MediaTypes: []domain.MediaType{domain.MediaAudiobook}, SelectedLibraries: []string{"books"}, Capabilities: SourceCapabilities{CompleteEnumeration: true}},
		EndpointURL:   "https://books.example.test", Credential: SourceCredentialChange{Replace: secrets.Envelope{Version: secrets.EnvelopeVersion, Ciphertext: make([]byte, 16), Identity: [32]byte{1}}}, AuditID: "4ecb0c32-e4b2-4b9a-ac03-337b48ea1111", OccurredAt: time.Now().UTC(),
	}
	if err := ValidateSourceConfigurationSave(valid); err != nil {
		t.Fatalf("replacement create validation: %v", err)
	}
	valid.Credential = SourceCredentialChange{Preserve: true}
	if err := ValidateSourceConfigurationSave(valid); !errors.Is(err, ErrInvalidSourceSave) {
		t.Fatalf("preserve create validation: %v", err)
	}
	valid.ExpectedRevision = 1
	if err := ValidateSourceConfigurationSave(valid); err != nil {
		t.Fatalf("preserve update validation: %v", err)
	}
	valid.Credential.Replace = secrets.Envelope{Version: secrets.EnvelopeVersion, Ciphertext: make([]byte, 16), Identity: [32]byte{1}}
	if err := ValidateSourceConfigurationSave(valid); !errors.Is(err, ErrInvalidSourceSave) {
		t.Fatalf("ambiguous credential operation validation: %v", err)
	}
}

func TestValidateSourceConfigurationSaveEndpointURLByteBoundary(t *testing.T) {
	endpoint := "https://" + strings.Repeat("a", SourceEndpointURLBytesMax-len("https://"))
	for _, tc := range []struct {
		name       string
		endpoint   string
		credential SourceCredentialChange
		valid      bool
	}{
		{
			name:     "replacement at durable maximum",
			endpoint: endpoint,
			credential: SourceCredentialChange{Replace: secrets.Envelope{
				Version: secrets.EnvelopeVersion, Ciphertext: make([]byte, 16), Identity: [32]byte{1},
			}},
			valid: true,
		},
		{
			name:     "replacement above durable maximum",
			endpoint: endpoint + "a",
			credential: SourceCredentialChange{Replace: secrets.Envelope{
				Version: secrets.EnvelopeVersion, Ciphertext: make([]byte, 16), Identity: [32]byte{1},
			}},
		},
		{
			name:       "preserve at durable maximum",
			endpoint:   endpoint,
			credential: SourceCredentialChange{Preserve: true},
			valid:      true,
		},
		{
			name:       "preserve above durable maximum",
			endpoint:   endpoint + "a",
			credential: SourceCredentialChange{Preserve: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			save := SourceConfigurationSave{
				Configuration: SourceConfiguration{ID: "05829685-9808-4a01-a0dc-744fa83a253f", Name: "Books", Kind: SourceAudiobookshelf, Enabled: true, MediaTypes: []domain.MediaType{domain.MediaAudiobook}, SelectedLibraries: []string{"books"}, Capabilities: SourceCapabilities{CompleteEnumeration: true}},
				EndpointURL:   tc.endpoint, Credential: tc.credential, ExpectedRevision: 1,
				AuditID: "4ecb0c32-e4b2-4b9a-ac03-337b48ea1111", OccurredAt: time.Now().UTC(),
			}
			err := ValidateSourceConfigurationSave(save)
			if tc.valid && err != nil {
				t.Fatalf("ValidateSourceConfigurationSave() error = %v", err)
			}
			if !tc.valid && !errors.Is(err, ErrInvalidSourceSave) {
				t.Fatalf("ValidateSourceConfigurationSave() error = %v, want invalid source save", err)
			}
		})
	}
}

func TestSourceNotFoundError(t *testing.T) {
	err := &SourceNotFoundError{SourceID: "missing"}
	if !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("SourceNotFoundError must support errors.Is: %v", err)
	}
}

type manualSyncRepositoryFake struct{ received ManualSyncRequest }

func (f *manualSyncRepositoryFake) RequestManualSync(_ context.Context, request ManualSyncRequest) (ManualSyncRequestResult, error) {
	f.received = request
	return ManualSyncRequestResult{Disposition: ManualSyncRecorded, PendingIntentID: request.ID}, nil
}

func TestManualSyncServiceValidatesAndCanonicalizesRequest(t *testing.T) {
	repository := &manualSyncRepositoryFake{}
	service := NewManualSyncService(repository)
	request := ManualSyncRequest{ID: " e6e79917-23c7-4f90-98c0-bc0a70cf2d6f ", SourceID: " 05829685-9808-4a01-a0dc-744fa83a253f ", RequestedBy: " 1dbf08bc-3c1f-4c2e-8eb9-771df4f729d5 ", RequestedAt: time.Now()}
	result, err := service.Request(context.Background(), request)
	if err != nil || result.Disposition != ManualSyncRecorded {
		t.Fatalf("Request() result=%+v error=%v", result, err)
	}
	if repository.received.ID != "e6e79917-23c7-4f90-98c0-bc0a70cf2d6f" || repository.received.SourceID != "05829685-9808-4a01-a0dc-744fa83a253f" || repository.received.RequestedBy != "1dbf08bc-3c1f-4c2e-8eb9-771df4f729d5" || repository.received.RequestedAt.Location() != time.UTC {
		t.Fatalf("canonical request=%+v", repository.received)
	}
}

func TestManualSyncServiceRejectsInvalidRequestWithoutRepositoryCall(t *testing.T) {
	repository := &manualSyncRepositoryFake{}
	_, err := NewManualSyncService(repository).Request(context.Background(), ManualSyncRequest{ID: "bad", SourceID: "bad", RequestedAt: time.Now()})
	if !errors.Is(err, ErrInvalidManualSyncRequest) || repository.received.ID != "" {
		t.Fatalf("Request() error=%v repository=%+v", err, repository.received)
	}
}

func TestCanonicalSourceConfigurationDoesNotMutateInput(t *testing.T) {
	configuration := SourceConfiguration{ID: " 05829685-9808-4a01-a0dc-744fa83a253f ", Name: " Source ", MediaTypes: []domain.MediaType{domain.MediaTV, domain.MediaMovie}, SelectedLibraries: []string{" z ", " a "}}
	canonical := CanonicalSourceConfiguration(configuration)
	if canonical.ID != "05829685-9808-4a01-a0dc-744fa83a253f" || canonical.MediaTypes[0] != domain.MediaMovie || canonical.SelectedLibraries[0] != "a" {
		t.Fatalf("canonical = %+v", canonical)
	}
	if configuration.ID != " 05829685-9808-4a01-a0dc-744fa83a253f " || configuration.MediaTypes[0] != domain.MediaTV || configuration.SelectedLibraries[0] != " z " {
		t.Fatalf("input mutated = %+v", configuration)
	}
}

func TestValidateAuthoritativeCycle(t *testing.T) {
	start := time.Now().Add(-time.Minute)
	valid := AuthoritativeCycle{ID: "e6e79917-23c7-4f90-98c0-bc0a70cf2d6f", SourceID: "05829685-9808-4a01-a0dc-744fa83a253f", StartedAt: start, CompletedAt: time.Now(), Complete: true, Events: []InventoryEvent{{Kind: ChangeAdded, SourceItemID: "item", MediaType: domain.MediaMovie}}}
	if err := ValidateAuthoritativeCycle(valid); err != nil {
		t.Fatalf("ValidateAuthoritativeCycle() error = %v", err)
	}
	valid.Complete = false
	if err := ValidateAuthoritativeCycle(valid); !errors.Is(err, ErrInvalidAuthoritativeCycle) {
		t.Fatalf("ValidateAuthoritativeCycle() error = %v", err)
	}
	valid.Complete = true
	valid.Events = append(valid.Events, InventoryEvent{Kind: ChangeUpdated, SourceItemID: "item", MediaType: domain.MediaMovie})
	if err := ValidateAuthoritativeCycle(valid); !errors.Is(err, ErrInvalidAuthoritativeCycle) {
		t.Fatalf("ValidateAuthoritativeCycle() duplicate source item error = %v", err)
	}
}

func TestValidateSourceStatus(t *testing.T) {
	now := time.Now()
	valid := SourceStatus{SourceID: "05829685-9808-4a01-a0dc-744fa83a253f", State: SourceSyncSucceeded, LastStartedAt: now.Add(-time.Minute), LastCompletedAt: now, LastSuccessfulAt: now}
	if err := ValidateSourceStatus(valid); err != nil {
		t.Fatalf("ValidateSourceStatus() error = %v", err)
	}
	valid.State, valid.FailureDetail = SourceSyncFailed, string(SourceFailureRequestTimedOut)
	if err := ValidateSourceStatus(valid); err != nil {
		t.Fatalf("ValidateSourceStatus() failed error = %v", err)
	}
	valid.FailureDetail = ""
	if err := ValidateSourceStatus(valid); !errors.Is(err, ErrInvalidAuthoritativeCycle) {
		t.Fatalf("ValidateSourceStatus() error = %v", err)
	}
}

func TestValidateSourceStatusTransition(t *testing.T) {
	valid := SourceStatusTransition{SourceID: "05829685-9808-4a01-a0dc-744fa83a253f", CycleID: "e6e79917-23c7-4f90-98c0-bc0a70cf2d6f", From: SourceSyncRunning, To: SourceSyncFailed, OccurredAt: time.Now(), FailureDetail: string(SourceFailureRequestTimedOut)}
	if err := ValidateSourceStatusTransition(valid); err != nil {
		t.Fatalf("ValidateSourceStatusTransition() error = %v", err)
	}
	valid.To, valid.FailureDetail = SourceSyncSucceeded, ""
	if err := ValidateSourceStatusTransition(valid); err != nil {
		t.Fatalf("ValidateSourceStatusTransition() completion error = %v", err)
	}
	valid.From, valid.To = SourceSyncIdle, SourceSyncSucceeded
	if err := ValidateSourceStatusTransition(valid); !errors.Is(err, ErrInvalidAuthoritativeCycle) {
		t.Fatalf("ValidateSourceStatusTransition() invalid sequence error = %v", err)
	}
}
