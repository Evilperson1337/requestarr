package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/domain"
)

// A diagnostics token (D043) is a long random bearer value a superuser makes
// so that a tool can read a short fixed list of read-only routes. It is shown
// once, stored only as a keyed hash (Hash, as sessions are), expires, and
// stops working when revoked or when its creator stops being a superuser who
// can sign in. This file is the service.
const (
	DiagnosticsTokenPrefix = "rqd_"
	// DiagnosticsTokenDefaultLifetime applies when the caller names none.
	DiagnosticsTokenDefaultLifetime = 30 * 24 * time.Hour
	// DiagnosticsTokenMaxLifetime is the longest a token may live.
	DiagnosticsTokenMaxLifetime = 90 * 24 * time.Hour
	// MaxActiveDiagnosticsTokens is how many unrevoked, unexpired tokens may exist at once.
	MaxActiveDiagnosticsTokens = 5
	// DiagnosticsTokenMaxNameLength is in characters.
	DiagnosticsTokenMaxNameLength = 80
	// DiagnosticsTokenTouchInterval is the least gap between writes of last_used_at.
	DiagnosticsTokenTouchInterval = time.Minute

	diagnosticsTokenBytes = 32
	// maxDiagnosticsTokenValueLength bounds a presented value before it is hashed.
	maxDiagnosticsTokenValueLength = 256
)

// Audit event types for diagnostics tokens. The rows carry the actor and the
// token's id, never its value.
const (
	AuditDiagnosticsTokenCreated = "diagnostics_token_created"
	AuditDiagnosticsTokenRevoked = "diagnostics_token_revoked"
)

var (
	ErrDiagnosticsTokenNotFound  = errors.New("diagnostics token not found")
	ErrDiagnosticsTokenLimit     = errors.New("there are already five active diagnostics tokens; revoke one first")
	ErrDiagnosticsTokenLifetime  = errors.New("a diagnostics token must expire within 90 days")
	ErrDiagnosticsTokenName      = errors.New("a diagnostics token needs a plain name of 1 to 80 characters")
	ErrDiagnosticsTokenForbidden = errors.New("only a superuser manages diagnostics tokens")
	// ErrDiagnosticsTokenRefused is wrapped by every refusal of a presented
	// token. Its text is the same whatever the reason; the reason is for the log.
	ErrDiagnosticsTokenRefused = errors.New("diagnostics token refused")
)

// DiagnosticsRefusalReason is the fixed vocabulary for logging a refused token.
type DiagnosticsRefusalReason string

const (
	DiagnosticsUnknown             DiagnosticsRefusalReason = "unknown"
	DiagnosticsExpired             DiagnosticsRefusalReason = "expired"
	DiagnosticsRevoked             DiagnosticsRefusalReason = "revoked"
	DiagnosticsCreatorNotSuperuser DiagnosticsRefusalReason = "creator_not_superuser"
)

// DiagnosticsRefusal is the error Lookup returns. It matches
// ErrDiagnosticsTokenRefused with errors.Is and never carries the value.
type DiagnosticsRefusal struct{ Reason DiagnosticsRefusalReason }

func (*DiagnosticsRefusal) Error() string { return ErrDiagnosticsTokenRefused.Error() }
func (*DiagnosticsRefusal) Is(target error) bool {
	return target == ErrDiagnosticsTokenRefused
}

// DiagnosticsToken is what is kept about a token other than its value.
type DiagnosticsToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedBy  string     `json:"createdBy"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

// Active reports whether the token is neither revoked nor expired at now.
func (t DiagnosticsToken) Active(now time.Time) bool {
	return t.RevokedAt == nil && now.Before(t.ExpiresAt)
}

// DiagnosticsTokenRecord is a token found by its hash, with whether its
// creator is still a superuser who can sign in.
type DiagnosticsTokenRecord struct {
	DiagnosticsToken
	CreatorCanSignIn bool
}

// DiagnosticsTokenStore is the storage the service needs. The audit event is
// written in the same transaction as the change it records.
type DiagnosticsTokenStore interface {
	// CreateDiagnosticsToken stores the token and its audit row, or returns
	// ErrDiagnosticsTokenLimit when MaxActiveDiagnosticsTokens are already active
	// at t.CreatedAt. The check and the insert are one step.
	CreateDiagnosticsToken(ctx context.Context, t DiagnosticsToken, hash []byte, audit AuditEvent) error
	// ListDiagnosticsTokens returns every unrevoked token, oldest first.
	ListDiagnosticsTokens(ctx context.Context) ([]DiagnosticsToken, error)
	// RevokeDiagnosticsToken revokes an unrevoked token and writes its audit row,
	// or returns ErrDiagnosticsTokenNotFound.
	RevokeDiagnosticsToken(ctx context.Context, id string, now time.Time, audit AuditEvent) error
	// DiagnosticsTokenByHash returns ErrDiagnosticsTokenNotFound for an unknown hash.
	DiagnosticsTokenByHash(ctx context.Context, hash []byte) (DiagnosticsTokenRecord, error)
	// TouchDiagnosticsToken sets last_used_at unless it was set within DiagnosticsTokenTouchInterval.
	TouchDiagnosticsToken(ctx context.Context, id string, now time.Time) error
}

// DiagnosticsTokens is the service. Secret is the session secret.
type DiagnosticsTokens struct {
	Store  DiagnosticsTokenStore
	Secret string
	Clock  func() time.Time
}

func (s *DiagnosticsTokens) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func isSuperuser(u User) bool { return domain.Role(u.Role).CanManageAccess() }

// Create makes a token for the acting superuser. lifetime 0 means the default
// of 30 days; anything above 90 days or below zero is refused. The returned
// value is the only copy there will ever be.
func (s *DiagnosticsTokens) Create(ctx context.Context, actor User, name string, lifetime time.Duration, remoteAddr string) (string, DiagnosticsToken, error) {
	if !isSuperuser(actor) {
		return "", DiagnosticsToken{}, ErrDiagnosticsTokenForbidden
	}
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > DiagnosticsTokenMaxNameLength || strings.IndexFunc(name, unicode.IsControl) >= 0 || !utf8.ValidString(name) {
		return "", DiagnosticsToken{}, ErrDiagnosticsTokenName
	}
	if lifetime == 0 {
		lifetime = DiagnosticsTokenDefaultLifetime
	}
	if lifetime < 0 || lifetime > DiagnosticsTokenMaxLifetime {
		return "", DiagnosticsToken{}, ErrDiagnosticsTokenLifetime
	}
	buf := make([]byte, diagnosticsTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", DiagnosticsToken{}, errors.New("could not make a diagnostics token")
	}
	value := DiagnosticsTokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	now := s.now()
	id, err := oidc.RandomUUID()
	if err != nil {
		return "", DiagnosticsToken{}, errors.New("could not make a diagnostics token")
	}
	tok := DiagnosticsToken{ID: id, Name: name, CreatedBy: actor.ID, CreatedAt: now, ExpiresAt: now.Add(lifetime)}
	audit := AuditEvent{Type: AuditDiagnosticsTokenCreated, UserID: actor.ID, RemoteAddr: remoteAddr, At: now}
	if err := s.Store.CreateDiagnosticsToken(ctx, tok, Hash(s.Secret, value), audit); err != nil {
		return "", DiagnosticsToken{}, err
	}
	return value, tok, nil
}

// List returns the unrevoked tokens, never a value or hash.
func (s *DiagnosticsTokens) List(ctx context.Context, actor User) ([]DiagnosticsToken, error) {
	if !isSuperuser(actor) {
		return nil, ErrDiagnosticsTokenForbidden
	}
	return s.Store.ListDiagnosticsTokens(ctx)
}

// Revoke ends a token at once. A token already revoked or unknown is
// ErrDiagnosticsTokenNotFound and writes no audit row.
func (s *DiagnosticsTokens) Revoke(ctx context.Context, actor User, id, remoteAddr string) error {
	if !isSuperuser(actor) {
		return ErrDiagnosticsTokenForbidden
	}
	now := s.now()
	return s.Store.RevokeDiagnosticsToken(ctx, id, now, AuditEvent{Type: AuditDiagnosticsTokenRevoked, UserID: actor.ID, RemoteAddr: remoteAddr, At: now})
}

// Lookup returns the token a presented value stands for, only while it is
// unexpired, unrevoked and its creator is a superuser who can sign in. Any
// other outcome is a *DiagnosticsRefusal (errors.Is ErrDiagnosticsTokenRefused)
// whose Reason is for the log; the error text is the same for all of them.
// A storage failure is returned as is.
func (s *DiagnosticsTokens) Lookup(ctx context.Context, value string) (DiagnosticsToken, error) {
	refuse := func(r DiagnosticsRefusalReason) (DiagnosticsToken, error) {
		return DiagnosticsToken{}, &DiagnosticsRefusal{Reason: r}
	}
	if !strings.HasPrefix(value, DiagnosticsTokenPrefix) || len(value) > maxDiagnosticsTokenValueLength {
		return refuse(DiagnosticsUnknown)
	}
	rec, err := s.Store.DiagnosticsTokenByHash(ctx, Hash(s.Secret, value))
	if errors.Is(err, ErrDiagnosticsTokenNotFound) {
		return refuse(DiagnosticsUnknown)
	}
	if err != nil {
		return DiagnosticsToken{}, err
	}
	switch {
	case rec.RevokedAt != nil:
		return refuse(DiagnosticsRevoked)
	case !s.now().Before(rec.ExpiresAt):
		return refuse(DiagnosticsExpired)
	case !rec.CreatorCanSignIn:
		return refuse(DiagnosticsCreatorNotSuperuser)
	}
	return rec.DiagnosticsToken, nil
}

// Touch records a use, at most once a minute.
func (s *DiagnosticsTokens) Touch(ctx context.Context, id string) error {
	return s.Store.TouchDiagnosticsToken(ctx, id, s.now())
}
