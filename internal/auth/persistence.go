package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrRuntimeConfigInvalid      = errors.New("runtime authentication configuration invalid")
	ErrRuntimeConfigUnavailable  = errors.New("runtime authentication configuration unavailable")
	ErrRuntimeConfigNotActivated = errors.New("runtime authentication configuration not activated")
	ErrRuntimeSecretUnavailable  = errors.New("runtime authentication secret unavailable")
	ErrRuntimeSecretInvalid      = errors.New("runtime authentication secret invalid")
)

// RuntimeConfig is the immutable normal-auth configuration selected at setup
// activation. Callers must validate every field against the normal-auth
// runtime policy before persisting a snapshot; database JSON shape and size
// constraints do not make configuration values semantically safe. This
// foundation intentionally does not perform activation.
type RuntimeConfig struct {
	AuthenticationRevision int64
	Issuer                 string
	ClientID               string
	ClientType             string
	Scopes                 []string
	GroupsClaim            string
	DefaultRole            Role
	GroupRoles             []GroupRoleMapping
	ActivatedAt            time.Time
}

// GroupRoleMapping is the typed immutable runtime representation of a group
// claim mapping. It intentionally does not reuse setup transport structures.
type GroupRoleMapping struct {
	Group string
	Role  Role
}

// Validate confirms that a persisted runtime snapshot is bounded and safe to
// use. Persisted JSON is untrusted even when database constraints exist.
func (c RuntimeConfig) Validate() error {
	if c.AuthenticationRevision < 1 || c.ActivatedAt.IsZero() ||
		!validRuntimeText(c.Issuer, 2048, 8192) || !validRuntimeText(c.ClientID, 256, 1024) ||
		!validRuntimeText(c.GroupsClaim, 128, 512) || !c.DefaultRole.Valid() ||
		(c.ClientType != "public" && c.ClientType != "confidential") ||
		len(c.Scopes) == 0 || len(c.Scopes) > 16 || len(c.GroupRoles) > 128 {
		return ErrRuntimeConfigInvalid
	}
	issuer, err := url.Parse(c.Issuer)
	if err != nil || !issuer.IsAbs() || issuer.Opaque != "" || issuer.User != nil || issuer.Host == "" ||
		(issuer.Scheme != "http" && issuer.Scheme != "https") || issuer.RawQuery != "" || issuer.Fragment != "" || runtimeAmbiguousIssuerPath(issuer) {
		return ErrRuntimeConfigInvalid
	}
	seenScopes := make(map[string]struct{}, len(c.Scopes))
	for _, scope := range c.Scopes {
		if !validRuntimeScope(scope) {
			return ErrRuntimeConfigInvalid
		}
		if _, exists := seenScopes[scope]; exists {
			return ErrRuntimeConfigInvalid
		}
		seenScopes[scope] = struct{}{}
	}
	if _, openID := seenScopes["openid"]; !openID {
		return ErrRuntimeConfigInvalid
	}
	seenGroups := make(map[string]struct{}, len(c.GroupRoles))
	for _, mapping := range c.GroupRoles {
		if !validRuntimeText(mapping.Group, 256, 1024) || !mapping.Role.Valid() {
			return ErrRuntimeConfigInvalid
		}
		if _, exists := seenGroups[mapping.Group]; exists {
			return ErrRuntimeConfigInvalid
		}
		seenGroups[mapping.Group] = struct{}{}
	}
	return nil
}

func validRuntimeText(value string, runes, bytes int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= runes && len(value) <= bytes && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validRuntimeScope(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if r <= 0x20 || r >= 0x7f || r == '"' || r == '\\' {
			return false
		}
	}
	return true
}

func runtimeAmbiguousIssuerPath(issuer *url.URL) bool {
	for _, segment := range strings.Split(issuer.EscapedPath(), "/") {
		lower := strings.ToLower(segment)
		if lower == "." || lower == ".." || lower == "%2e" || lower == "%2e%2e" || strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
			return true
		}
	}
	return false
}

// LoginTransaction is the persisted, one-time authorization-code correlation.
type LoginTransaction struct {
	ID                     string
	StateDigest            [32]byte
	NonceDigest            [32]byte
	PKCEVerifierDigest     [32]byte
	AuthenticationRevision int64
	CreatedAt              time.Time
	ExpiresAt              time.Time
}

// Session is an authenticated browser session represented by a digest only.
type Session struct {
	ID                     string
	TokenDigest            [32]byte
	UserID                 string
	AuthenticationRevision int64
	CreatedAt              time.Time
	ExpiresAt              time.Time
	RevokedAt              *time.Time
}

// User is the locally provisioned representation of a verified OIDC identity.
// Provider tokens and claims beyond these presentation fields are never stored.
type User struct {
	ID          string
	Issuer      string
	Subject     string
	Email       string
	DisplayName string
	Role        Role
}

// Repository defines only the normal-auth persistence primitives needed by the
// next staged implementation. It deliberately exposes no HTTP or provider API.
//
// ConsumeLoginTransaction must atomically find a transaction by state digest,
// require consumed_at IS NULL and expires_at > clock_timestamp(), mark it consumed, and
// return the consumed row. Concurrent calls for the same digest must allow at
// most one successful consumption. ReadSession must return a session only when
// revoked_at IS NULL and expires_at > clock_timestamp(). Implementations must
// use authoritative database time; cleanup is never a correctness mechanism.
type Repository interface {
	ReadRuntimeConfig(context.Context) (RuntimeConfig, error)
	CreateLoginTransaction(context.Context, LoginTransaction) error
	// ConsumeLoginTransaction has the atomic one-time-use contract described above.
	ConsumeLoginTransaction(context.Context, [32]byte, time.Time) (LoginTransaction, error)
	CreateSession(context.Context, Session) error
	// ReadSession has the live-session-only contract described above.
	ReadSession(context.Context, [32]byte, time.Time) (Session, error)
	ReadUser(context.Context, string) (User, error)
	RevokeSession(context.Context, string, time.Time) error
	UpsertUser(context.Context, User, time.Time) (User, error)
	CreateAuditEvent(context.Context, AuditEvent) error
}

// AuditEvent records a non-secret normal-auth state transition.
type AuditEvent struct {
	ID        string
	EventType string
	UserID    string
	SessionID string
	CreatedAt time.Time
}
