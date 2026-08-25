package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/secrets"
)

var ErrSourceNotFound = errors.New("inventory source not found")

// SourceNotFoundError identifies the absent source without exposing database
// details. Callers can use errors.Is(err, ErrSourceNotFound) for HTTP mapping.
type SourceNotFoundError struct{ SourceID string }

func (e *SourceNotFoundError) Error() string {
	return fmt.Sprintf("%s: %s", ErrSourceNotFound, e.SourceID)
}
func (e *SourceNotFoundError) Is(target error) bool { return target == ErrSourceNotFound }

// SourceCredentialChange deliberately has no clear operation. A credential is
// either retained byte-for-byte or replaced by a new encrypted envelope.
type SourceCredentialChange struct {
	Preserve bool
	Replace  secrets.Envelope
}

func (c SourceCredentialChange) replacing() bool { return !c.Preserve }

// SourceConfigurationSave is the secret-safe configuration write contract.
// IDs, audit evidence, actor, and timestamp are trusted application inputs.
type SourceConfigurationSave struct {
	Configuration    SourceConfiguration
	EndpointURL      string
	Credential       SourceCredentialChange
	ExpectedRevision int64
	ActorID          string
	AuditID          string
	OccurredAt       time.Time
}

// SourceConfigurationView is safe to return from application query methods.
// It intentionally contains neither credential plaintext nor its envelope or
// stable identity.
type SourceConfigurationView struct {
	Configuration        SourceConfiguration
	EndpointURL          string
	Revision             int64
	CredentialConfigured bool
	Status               SourceStatus
}

type SourceConfigurationQueryRepository interface {
	ListSourceConfigurations(context.Context) ([]SourceConfigurationView, error)
	GetSourceConfiguration(context.Context, string) (SourceConfigurationView, error)
}

type SourceConfigurationSaveRepository interface {
	SaveSourceConfiguration(context.Context, SourceConfigurationSave) (SourceSaveResult, error)
}

func ValidateSourceConfigurationSave(save SourceConfigurationSave) error {
	if save.Credential.Preserve && (save.Credential.Replace.Version != 0 || len(save.Credential.Replace.Ciphertext) != 0 || save.Credential.Replace.Identity != ([32]byte{})) {
		return fmt.Errorf("%w: credential operation is ambiguous", ErrInvalidSourceSave)
	}
	legacy := SourceSave{Configuration: save.Configuration, EndpointURL: save.EndpointURL, ExpectedRevision: save.ExpectedRevision, ActorID: save.ActorID, AuditID: save.AuditID, OccurredAt: save.OccurredAt}
	if save.Credential.replacing() {
		legacy.Credential = save.Credential.Replace
	}
	if save.ExpectedRevision == 0 && save.Credential.Preserve {
		return fmt.Errorf("%w: creation requires a replacement credential", ErrInvalidSourceSave)
	}
	if save.Credential.Preserve {
		// Validation shared with SourceSave except that an existing envelope is
		// not supplied for a preserve request.
		legacy.Credential = secrets.Envelope{Version: secrets.EnvelopeVersion, Ciphertext: make([]byte, 16), Identity: [32]byte{1}}
	}
	return ValidateSourceSave(legacy)
}
