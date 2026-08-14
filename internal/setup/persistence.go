package setup

import (
	"context"
	"errors"
	"time"
)

// Digest is a keyed SHA-256 identifier. Its fixed size prevents persistence
// callers from accidentally passing raw bootstrap, session, or peer material.
type Digest [32]byte

// PersistenceID is opaque UUID storage supplied by the application boundary.
type PersistenceID [16]byte

type ExchangeOutcome uint8

const (
	ExchangeSessionCreated ExchangeOutcome = iota + 1
	ExchangeTokenInvalid
	ExchangeRateLimited
	ExchangeSetupCompleted
	ExchangeSetupLocked
)

type RateScope string

const (
	RateScopePeer   RateScope = "peer"
	RateScopeGlobal RateScope = "global"
)

type ExchangeRequest struct {
	PeerKey       Digest
	GlobalKey     Digest
	Locked        bool
	TokenValid    bool
	Verify        func() bool
	Material      func() (ExchangeMaterial, error)
	SessionID     PersistenceID
	SessionDigest Digest
	EventID       PersistenceID
	PeerEventID   PersistenceID
	GlobalEventID PersistenceID
}

// ExchangeMaterial is generated lazily inside the persistence transaction so
// completion, lock, and exhausted-limit outcomes do not depend on randomness.
type ExchangeMaterial struct {
	SessionID     PersistenceID
	SessionDigest Digest
	EventID       PersistenceID
	PeerEventID   PersistenceID
	GlobalEventID PersistenceID
}

type ExchangeResult struct {
	Outcome    ExchangeOutcome
	SessionID  PersistenceID
	ExpiresAt  time.Time
	RetryAfter time.Duration
}

type Session struct {
	ID        PersistenceID
	CreatedAt time.Time
	ExpiresAt time.Time
}

type CompletionRequest struct {
	RevocationEventID PersistenceID
	CompletionEventID PersistenceID
}

type CompletionResult struct {
	RevokedSessions int64
}

type CleanupResult struct {
	RateLimits int64
	Sessions   int64
}

var (
	ErrPersistenceUnavailable  = errors.New("setup persistence unavailable")
	ErrPersistenceCanceled     = errors.New("setup persistence canceled")
	ErrInvalidPersistenceInput = errors.New("invalid setup persistence input")
	ErrSetupAlreadyComplete    = errors.New("setup already complete")
)

// BootstrapPersistence owns setup security transactions; no pool or generic
// transaction leaks across this consumer-owned boundary.
type BootstrapPersistence interface {
	Exchange(context.Context, ExchangeRequest) (ExchangeResult, error)
	ValidateSession(context.Context, Digest) (Session, error)
	RevokeSession(context.Context, PersistenceID, PersistenceID) (bool, error)
	Complete(context.Context, CompletionRequest) (CompletionResult, error)
	Cleanup(context.Context) (CleanupResult, error)
}
