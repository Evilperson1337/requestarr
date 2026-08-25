package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

const (
	LoginTransactionLifetime = 10 * time.Minute
	SessionLifetime          = 12 * time.Hour
)

var (
	ErrLoginUnavailable = errors.New("authentication login unavailable")
	ErrLoginInvalid     = errors.New("authentication login invalid")
	ErrSessionRequired  = errors.New("authentication session required")
)

// LoginCookie is the short-lived browser-held secret portion of an OIDC login.
// It is always encrypted and authenticated before it is written to a cookie.
type LoginCookie struct {
	State        State
	Nonce        Nonce
	PKCEVerifier PKCEVerifier
	ExpiresAt    time.Time
}

// Identity contains the validated, minimal OIDC claims required for JIT
// provisioning. The verifier is responsible for issuer, audience, nonce, time,
// and RS256 signature validation before returning it.
type Identity struct {
	Issuer      string
	Subject     string
	Email       string
	DisplayName string
	Groups      []string
}

// BrowserProvider is the narrow protocol boundary for authorization-code login.
type BrowserProvider interface {
	AuthorizationURL(context.Context, RuntimeConfig, State, Nonce, PKCEVerifier, string) (string, error)
	ExchangeCode(context.Context, RuntimeConfig, string, PKCEVerifier, Nonce, string) (Identity, error)
}

// BrowserService coordinates browser login without depending on a specific
// transport or persistence implementation.
type BrowserService struct {
	repository Repository
	provider   BrowserProvider
	random     io.Reader
	now        func() time.Time
}

func NewBrowserService(repository Repository, provider BrowserProvider) *BrowserService {
	return &BrowserService{repository: repository, provider: provider, random: rand.Reader, now: time.Now}
}

// Start creates a one-time transaction and authorization request URL.
func (s *BrowserService) Start(ctx context.Context, config RuntimeConfig, redirectURI string) (LoginCookie, string, error) {
	if s == nil || s.repository == nil || s.provider == nil || s.random == nil || config.Validate() != nil {
		return LoginCookie{}, "", ErrLoginUnavailable
	}
	state, err := randomOpaque(s.random)
	if err != nil {
		return LoginCookie{}, "", ErrLoginUnavailable
	}
	nonce, err := randomOpaque(s.random)
	if err != nil {
		return LoginCookie{}, "", ErrLoginUnavailable
	}
	verifier, err := randomPKCEVerifier(s.random)
	if err != nil {
		return LoginCookie{}, "", ErrLoginUnavailable
	}
	now := s.now().UTC()
	cookie := LoginCookie{State: State(state), Nonce: Nonce(nonce), PKCEVerifier: PKCEVerifier(verifier), ExpiresAt: now.Add(LoginTransactionLifetime)}
	transaction := LoginTransaction{ID: randomUUID(s.random), StateDigest: cookie.State.Digest(), NonceDigest: cookie.Nonce.Digest(), PKCEVerifierDigest: cookie.PKCEVerifier.Digest(), AuthenticationRevision: config.AuthenticationRevision, CreatedAt: now, ExpiresAt: cookie.ExpiresAt}
	if err := s.repository.CreateLoginTransaction(ctx, transaction); err != nil {
		return LoginCookie{}, "", ErrLoginUnavailable
	}
	url, err := s.provider.AuthorizationURL(ctx, config, cookie.State, cookie.Nonce, cookie.PKCEVerifier, redirectURI)
	if err != nil {
		return LoginCookie{}, "", ErrLoginUnavailable
	}
	_ = s.repository.CreateAuditEvent(ctx, AuditEvent{ID: randomUUID(s.random), EventType: "login_started", CreatedAt: now})
	return cookie, url, nil
}

// Complete consumes the correlation transaction once, exchanges the code, JIT
// provisions the verified identity, and creates an opaque local session.
func (s *BrowserService) Complete(ctx context.Context, config RuntimeConfig, cookie LoginCookie, callbackState, code, redirectURI string) (SessionToken, User, time.Time, error) {
	if s == nil || s.repository == nil || s.provider == nil || config.Validate() != nil || callbackState != string(cookie.State) || strings.TrimSpace(code) == "" || cookie.ExpiresAt.Before(s.now()) {
		return "", User{}, time.Time{}, ErrLoginInvalid
	}
	if _, err := ParseState(callbackState); err != nil {
		return "", User{}, time.Time{}, ErrLoginInvalid
	}
	now := s.now().UTC()
	tx, err := s.repository.ConsumeLoginTransaction(ctx, cookie.State.Digest(), now)
	if err != nil || tx.AuthenticationRevision != config.AuthenticationRevision || tx.NonceDigest != cookie.Nonce.Digest() || tx.PKCEVerifierDigest != cookie.PKCEVerifier.Digest() {
		return "", User{}, time.Time{}, ErrLoginInvalid
	}
	identity, err := s.provider.ExchangeCode(ctx, config, code, cookie.PKCEVerifier, cookie.Nonce, redirectURI)
	if err != nil {
		_ = s.repository.CreateAuditEvent(ctx, AuditEvent{ID: randomUUID(s.random), EventType: "login_failed", CreatedAt: now})
		return "", User{}, time.Time{}, ErrLoginInvalid
	}
	role := roleFor(config, identity.Groups)
	user, err := s.repository.UpsertUser(ctx, User{ID: randomUUID(s.random), Issuer: identity.Issuer, Subject: identity.Subject, Email: identity.Email, DisplayName: identity.DisplayName, Role: role}, now)
	if err != nil {
		return "", User{}, time.Time{}, ErrLoginUnavailable
	}
	tokenRaw, err := randomOpaque(s.random)
	if err != nil {
		return "", User{}, time.Time{}, ErrLoginUnavailable
	}
	token := SessionToken(tokenRaw)
	expires := now.Add(SessionLifetime)
	session := Session{ID: randomUUID(s.random), TokenDigest: token.Digest(), UserID: user.ID, AuthenticationRevision: config.AuthenticationRevision, CreatedAt: now, ExpiresAt: expires}
	if err := s.repository.CreateSession(ctx, session); err != nil {
		return "", User{}, time.Time{}, ErrLoginUnavailable
	}
	_ = s.repository.CreateAuditEvent(ctx, AuditEvent{ID: randomUUID(s.random), EventType: "login_succeeded", UserID: user.ID, SessionID: session.ID, CreatedAt: now})
	return token, user, expires, nil
}

func (s *BrowserService) Session(ctx context.Context, token SessionToken) (Principal, string, time.Time, error) {
	if s == nil || s.repository == nil {
		return Principal{}, "", time.Time{}, ErrSessionRequired
	}
	session, err := s.repository.ReadSession(ctx, token.Digest(), s.now().UTC())
	if err != nil {
		return Principal{}, "", time.Time{}, ErrSessionRequired
	}
	user, err := s.repository.ReadUser(ctx, session.UserID)
	if err != nil || !user.Role.Valid() {
		return Principal{}, "", time.Time{}, ErrSessionRequired
	}
	return Principal{UserID: user.ID, Role: user.Role}, session.ID, session.ExpiresAt, nil
}

func (s *BrowserService) Logout(ctx context.Context, sessionID string) error {
	if s == nil || s.repository == nil || sessionID == "" {
		return ErrSessionRequired
	}
	if err := s.repository.RevokeSession(ctx, sessionID, s.now().UTC()); err != nil {
		return ErrSessionRequired
	}
	_ = s.repository.CreateAuditEvent(ctx, AuditEvent{ID: randomUUID(s.random), EventType: "session_revoked", SessionID: sessionID, CreatedAt: s.now().UTC()})
	return nil
}

func roleFor(config RuntimeConfig, groups []string) Role {
	role := config.DefaultRole
	for _, mapping := range config.GroupRoles {
		if slices.Contains(groups, mapping.Group) && roleRank(mapping.Role) > roleRank(role) {
			role = mapping.Role
		}
	}
	return role
}

func roleRank(role Role) int {
	return map[Role]int{RoleViewer: 1, RoleRequester: 2, RoleAdmin: 3}[role]
}

func randomOpaque(random io.Reader) (string, error) {
	value := make([]byte, opaqueMaterialBytes)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", fmt.Errorf("read random opaque value: %w", err)
	}
	return rawBase64URL.EncodeToString(value), nil
}

func randomPKCEVerifier(random io.Reader) (string, error) {
	value := make([]byte, 48)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", fmt.Errorf("read random PKCE verifier: %w", err)
	}
	return rawBase64URL.EncodeToString(value), nil
}

func randomUUID(random io.Reader) string {
	var value [16]byte
	if _, err := io.ReadFull(random, value[:]); err != nil {
		return ""
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[:4], value[4:6], value[6:8], value[8:10], value[10:])
}

// PKCEChallenge calculates the S256 code challenge for an already validated verifier.
func PKCEChallenge(verifier PKCEVerifier) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
