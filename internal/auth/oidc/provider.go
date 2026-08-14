package oidc

import "context"

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	GroupsClaim  string
	GroupRoleMap map[string]string
}

type Identity struct {
	Subject     string
	Email       string
	DisplayName string
	Groups      []string
}

// Provider boundary. The concrete implementation should use standards-compliant
// OIDC discovery and Authorization Code + PKCE where applicable. Keycloak is the
// primary V1 compatibility target.
type Provider interface {
	Validate(context.Context, Config) error
	AuthorizationURL(state, nonce, pkceChallenge string) (string, error)
}
