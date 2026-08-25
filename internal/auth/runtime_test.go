package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type runtimeConfigStub struct {
	config RuntimeConfig
	err    error
}

func (s runtimeConfigStub) ReadRuntimeConfig(context.Context) (RuntimeConfig, error) {
	return s.config, s.err
}

type runtimeSecretStub struct {
	value []byte
	err   error
}

func (s *runtimeSecretStub) UseRuntimeClientSecret(_ context.Context, use func([]byte) error) error {
	if s.err != nil {
		return s.err
	}
	err := use(s.value)
	for i := range s.value {
		s.value[i] = 0
	}
	return err
}

func validRuntimeConfig(clientType string) RuntimeConfig {
	return RuntimeConfig{AuthenticationRevision: 1, Issuer: "https://issuer.example/realm", ClientID: "requestarr", ClientType: clientType, Scopes: []string{"openid", "groups"}, GroupsClaim: "groups", DefaultRole: RoleViewer, GroupRoles: []GroupRoleMapping{{Group: "admins", Role: RoleAdmin}}, ActivatedAt: time.Now()}
}

func TestRuntimeConfigurationServicePublicDoesNotReadSecret(t *testing.T) {
	config := validRuntimeConfig("public")
	service := NewRuntimeConfigurationService(runtimeConfigStub{config: config}, nil)
	got, err := service.VerifyActivated(context.Background())
	if err != nil || got.ClientType != "public" || got.GroupRoles[0].Role != RoleAdmin {
		t.Fatalf("VerifyActivated() = %#v, %v", got, err)
	}
}

func TestRuntimeConfigurationServiceConfidentialUsesAndClearsSecret(t *testing.T) {
	secret := &runtimeSecretStub{value: []byte("runtime-secret")}
	service := NewRuntimeConfigurationService(runtimeConfigStub{config: validRuntimeConfig("confidential")}, secret)
	if _, err := service.VerifyActivated(context.Background()); err != nil {
		t.Fatalf("VerifyActivated() error = %v", err)
	}
	for _, value := range secret.value {
		if value != 0 {
			t.Fatal("runtime secret was retained after use")
		}
	}
}

func TestRuntimeConfigurationServiceRejectsInvalidOrMissingRuntimeSecret(t *testing.T) {
	service := NewRuntimeConfigurationService(runtimeConfigStub{config: validRuntimeConfig("confidential")}, &runtimeSecretStub{err: errors.New("not found")})
	if _, err := service.VerifyActivated(context.Background()); !errors.Is(err, ErrRuntimeSecretUnavailable) {
		t.Fatalf("missing secret error = %v", err)
	}
	invalid := validRuntimeConfig("public")
	invalid.GroupRoles = append(invalid.GroupRoles, GroupRoleMapping{Group: "admins", Role: RoleViewer})
	if _, err := NewRuntimeConfigurationService(runtimeConfigStub{config: invalid}, nil).VerifyActivated(context.Background()); !errors.Is(err, ErrRuntimeConfigInvalid) {
		t.Fatalf("invalid mapping error = %v", err)
	}
}
