package setup

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/auth/oidc"
)

const AuthenticationValidationPolicyVersion = oidc.PolicyVersion

type AuthenticationDiscoverer interface {
	Discover(context.Context, oidc.DiscoveryInput) (oidc.Evidence, error)
}

type AuthenticationValidationSnapshot struct {
	Outcome        AuthenticationValidationOutcome
	Authentication Authentication
	Revision       int64
	RetryAfter     time.Duration
}

type AuthenticationValidationPersistence interface {
	ReserveAuthenticationValidation(context.Context, Digest, Digest, int64) (AuthenticationValidationSnapshot, error)
	FinishAuthenticationValidation(context.Context, Digest, AuthenticationValidationSnapshot, *oidc.Evidence, oidc.FailureKind) (AuthenticationValidationResult, error)
}

type AuthenticationValidationOutcome uint8

const (
	AuthenticationValidationSucceeded AuthenticationValidationOutcome = iota + 1
	AuthenticationValidationSessionRequired
	AuthenticationValidationCompleted
	AuthenticationValidationStale
	AuthenticationValidationRequired
	AuthenticationValidationDestination
	AuthenticationValidationUnreachable
	AuthenticationValidationRateLimited
	AuthenticationValidationUnavailable
)

type AuthenticationValidationResult struct {
	Outcome    AuthenticationValidationOutcome
	Revision   int64
	Evidence   *AuthenticationValidationEvidence
	RetryAfter time.Duration
}

type AuthenticationValidationService struct {
	persistence AuthenticationValidationPersistence
	discovery   AuthenticationDiscoverer
	key         Digest
	configured  bool
}

func NewAuthenticationValidationService(persistence AuthenticationValidationPersistence, discovery AuthenticationDiscoverer, token [32]byte, configured bool) *AuthenticationValidationService {
	service := &AuthenticationValidationService{persistence: persistence, discovery: discovery, configured: configured}
	if configured {
		service.key = keyedDigest(token[:], []byte("requestarr/setup/v1/key"))
	}
	return service
}

func (s *AuthenticationValidationService) Validate(ctx context.Context, rawSession string, revision int64) AuthenticationValidationResult {
	if revision <= 0 {
		return AuthenticationValidationResult{Outcome: AuthenticationValidationRequired}
	}
	if s == nil {
		return AuthenticationValidationResult{Outcome: AuthenticationValidationUnavailable}
	}
	digest, ok := digestRawSession(s.key, rawSession)
	if !s.configured || !ok {
		return AuthenticationValidationResult{Outcome: AuthenticationValidationSessionRequired}
	}
	if ctx.Err() != nil || s.persistence == nil || s.discovery == nil {
		return AuthenticationValidationResult{Outcome: AuthenticationValidationUnavailable}
	}
	global := keyedDigest(s.key[:], []byte("requestarr/setup/v1/rate/authentication-validation/global"))
	snapshot, err := s.persistence.ReserveAuthenticationValidation(ctx, digest, global, revision)
	if err != nil {
		return AuthenticationValidationResult{Outcome: AuthenticationValidationUnavailable}
	}
	if snapshot.Outcome != AuthenticationValidationSucceeded {
		return AuthenticationValidationResult{Outcome: snapshot.Outcome, Revision: snapshot.Revision, RetryAfter: snapshot.RetryAfter}
	}
	clientType := oidc.ClientPublic
	if snapshot.Authentication.ClientType == ClientTypeConfidential {
		clientType = oidc.ClientConfidential
	}
	evidence, discoveryErr := s.discovery.Discover(ctx, oidc.DiscoveryInput{Issuer: snapshot.Authentication.Issuer, ClientType: clientType})
	failureKind := oidc.FailureKind(0)
	var evidencePointer *oidc.Evidence
	if discoveryErr == nil {
		evidencePointer = &evidence
	} else {
		var safeFailure *oidc.Failure
		if !errors.As(discoveryErr, &safeFailure) {
			return AuthenticationValidationResult{Outcome: AuthenticationValidationUnavailable}
		}
		failureKind = safeFailure.Kind
	}
	result, err := s.persistence.FinishAuthenticationValidation(ctx, digest, snapshot, evidencePointer, failureKind)
	if err != nil {
		return AuthenticationValidationResult{Outcome: AuthenticationValidationUnavailable}
	}
	return result
}

func digestRawSession(key Digest, raw string) (Digest, bool) {
	if len(raw) != BootstrapTokenSize {
		return Digest{}, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != bootstrapTokenBytes || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return Digest{}, false
	}
	var session [bootstrapTokenBytes]byte
	copy(session[:], decoded)
	return sessionDigest(key, session), true
}
