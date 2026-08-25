package auth

import (
	"context"
	"errors"
	"unicode/utf8"
)

// RuntimeSecretUser permits narrowly scoped use of a runtime client secret.
// Implementations must clear plaintext before this method returns.
type RuntimeSecretUser interface {
	UseRuntimeClientSecret(context.Context, func([]byte) error) error
}

// RuntimeConfigReader provides the one persistence operation needed during
// runtime activation verification.
type RuntimeConfigReader interface {
	ReadRuntimeConfig(context.Context) (RuntimeConfig, error)
}

// RuntimeConfigurationService verifies the persisted snapshot without retaining
// credentials or exposing any browser-login behavior.
type RuntimeConfigurationService struct {
	repository RuntimeConfigReader
	secrets    RuntimeSecretUser
}

func NewRuntimeConfigurationService(repository RuntimeConfigReader, secrets RuntimeSecretUser) *RuntimeConfigurationService {
	return &RuntimeConfigurationService{repository: repository, secrets: secrets}
}

// VerifyActivated reads and validates an activated snapshot. Confidential
// clients prove that their runtime-only secret can be decrypted and used.
func (s *RuntimeConfigurationService) VerifyActivated(ctx context.Context) (RuntimeConfig, error) {
	if err := ctx.Err(); err != nil {
		return RuntimeConfig{}, ErrRuntimeConfigUnavailable
	}
	if s == nil || s.repository == nil {
		return RuntimeConfig{}, ErrRuntimeConfigUnavailable
	}
	config, err := s.repository.ReadRuntimeConfig(ctx)
	if err != nil {
		return RuntimeConfig{}, err
	}
	if err := config.Validate(); err != nil {
		return RuntimeConfig{}, err
	}
	if config.ClientType == "public" {
		return config, nil
	}
	if s.secrets == nil {
		return RuntimeConfig{}, ErrRuntimeSecretUnavailable
	}
	err = s.secrets.UseRuntimeClientSecret(ctx, func(value []byte) error {
		if len(value) == 0 || len(value) > 8192 || !utf8ValidNoControl(value) {
			return ErrRuntimeSecretInvalid
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrRuntimeSecretInvalid) {
			return RuntimeConfig{}, err
		}
		return RuntimeConfig{}, ErrRuntimeSecretUnavailable
	}
	return config, nil
}

func utf8ValidNoControl(value []byte) bool {
	for len(value) > 0 {
		r, size := utf8.DecodeRune(value)
		if size == 0 || (r == utf8.RuneError && size == 1) || r < 0x20 || r == 0x7f {
			return false
		}
		value = value[size:]
	}
	return true
}
