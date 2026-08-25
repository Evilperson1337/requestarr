// Package auth contains transport- and storage-independent normal-auth rules.
package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const opaqueMaterialBytes = 32

var rawBase64URL = base64.RawURLEncoding

// Role controls the actions a principal may perform.
type Role string

const (
	RoleViewer    Role = "viewer"
	RoleRequester Role = "requester"
	RoleAdmin     Role = "admin"
)

// Valid reports whether r is a supported persisted role.
func (r Role) Valid() bool {
	return r == RoleViewer || r == RoleRequester || r == RoleAdmin
}

// Permission is an application capability, not an HTTP route.
type Permission string

const (
	PermissionViewRequests   Permission = "requests:view"
	PermissionCreateRequests Permission = "requests:create"
	PermissionReviewRequests Permission = "requests:review"
	PermissionManageSettings Permission = "settings:manage"
)

// Principal is the authenticated, authorization-ready identity.
type Principal struct {
	UserID string
	Role   Role
}

// Valid rejects incomplete or unsupported principals.
func (p Principal) Valid() bool {
	return strings.TrimSpace(p.UserID) != "" && p.Role.Valid()
}

// Allows reports whether the principal's role grants permission.
func (p Principal) Allows(permission Permission) bool {
	if !p.Valid() {
		return false
	}
	switch p.Role {
	case RoleViewer:
		return permission == PermissionViewRequests
	case RoleRequester:
		return permission == PermissionViewRequests || permission == PermissionCreateRequests
	case RoleAdmin:
		return permission == PermissionViewRequests || permission == PermissionCreateRequests || permission == PermissionReviewRequests || permission == PermissionManageSettings
	default:
		return false
	}
}

// State is a one-time opaque login correlation value.
type State string

// Nonce binds an ID token to the login transaction that initiated it.
type Nonce string

// SessionToken is the opaque browser credential for an authenticated session.
type SessionToken string

// PKCEVerifier is the RFC 7636 code verifier retained only until callback use.
type PKCEVerifier string

// ParseState validates a canonical 32-byte unpadded base64url state value.
func ParseState(value string) (State, error) {
	if err := validateOpaque(value); err != nil {
		return "", errors.New("auth state invalid")
	}
	return State(value), nil
}

// ParseNonce validates a canonical 32-byte unpadded base64url nonce value.
func ParseNonce(value string) (Nonce, error) {
	if err := validateOpaque(value); err != nil {
		return "", errors.New("auth nonce invalid")
	}
	return Nonce(value), nil
}

// ParseSessionToken validates a canonical 32-byte unpadded base64url session token.
func ParseSessionToken(value string) (SessionToken, error) {
	if err := validateOpaque(value); err != nil {
		return "", errors.New("auth session token invalid")
	}
	return SessionToken(value), nil
}

// ParsePKCEVerifier validates the unreserved RFC 7636 verifier alphabet and size.
func ParsePKCEVerifier(value string) (PKCEVerifier, error) {
	if len(value) < 43 || len(value) > 128 {
		return "", errors.New("PKCE verifier invalid")
	}
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~') {
			return "", errors.New("PKCE verifier invalid")
		}
	}
	return PKCEVerifier(value), nil
}

// Digest produces a domain-separated, fixed-length value safe for persistence.
func Digest(label, value string) [sha256.Size]byte {
	return sha256.Sum256([]byte("requestarr/auth/v1/" + label + "\x00" + value))
}

// Digest returns the persistable state digest.
func (s State) Digest() [sha256.Size]byte { return Digest("state", string(s)) }

// Digest returns the persistable nonce digest.
func (n Nonce) Digest() [sha256.Size]byte { return Digest("nonce", string(n)) }

// Digest returns the persistable session-token digest.
func (t SessionToken) Digest() [sha256.Size]byte { return Digest("session", string(t)) }

// Digest returns the persistable PKCE verifier digest.
func (v PKCEVerifier) Digest() [sha256.Size]byte { return Digest("pkce-verifier", string(v)) }

func validateOpaque(value string) error {
	if len(value) != rawBase64URL.EncodedLen(opaqueMaterialBytes) {
		return errors.New("opaque value invalid")
	}
	decoded, err := rawBase64URL.DecodeString(value)
	if err != nil || len(decoded) != opaqueMaterialBytes || rawBase64URL.EncodeToString(decoded) != value {
		return errors.New("opaque value invalid")
	}
	return nil
}
