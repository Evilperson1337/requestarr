package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"git.example.com/evilperson1337/Requestarr/internal/setup"
)

const (
	peerFailureLimit   = int32(5)
	globalFailureLimit = int32(100)
	cleanupLimit       = int64(1000)
)

type SetupBootstrapRepository struct {
	pool *Pool
}

func NewSetupBootstrapRepository(pool *Pool) *SetupBootstrapRepository {
	return &SetupBootstrapRepository{pool: pool}
}

func (r *SetupBootstrapRepository) Exchange(ctx context.Context, request setup.ExchangeRequest) (result setup.ExchangeResult, err error) {
	if err := ctx.Err(); err != nil {
		return result, setup.ErrPersistenceCanceled
	}
	if !r.available() {
		return result, setup.ErrPersistenceUnavailable
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var complete bool
	if err = tx.QueryRow(ctx, `SELECT complete FROM setup_state WHERE singleton = TRUE FOR UPDATE`).Scan(&complete); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if complete {
		if err = tx.Commit(ctx); err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		return setup.ExchangeResult{Outcome: setup.ExchangeSetupCompleted}, nil
	}
	if request.Locked {
		if err = tx.Commit(ctx); err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		return setup.ExchangeResult{Outcome: setup.ExchangeSetupLocked}, nil
	}

	windowStart, retryAfter, err := exchangeWindow(ctx, tx)
	if err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	limited, err := anyRateLimitExhausted(ctx, tx, request, windowStart)
	if err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if limited {
		if err = tx.Commit(ctx); err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		return setup.ExchangeResult{Outcome: setup.ExchangeRateLimited, RetryAfter: retryAfter}, nil
	}

	tokenValid := request.TokenValid
	if request.Verify != nil {
		tokenValid = request.Verify()
	}
	if !tokenValid {
		peerCount, peerLimited, err := incrementRateLimit(ctx, tx, setup.RateScopePeer, request.PeerKey, windowStart, peerFailureLimit)
		if err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		globalCount, globalLimited, err := incrementRateLimit(ctx, tx, setup.RateScopeGlobal, request.GlobalKey, windowStart, globalFailureLimit)
		if err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		if peerLimited || globalLimited {
			if err = tx.Rollback(ctx); err != nil {
				return result, classifySetupPersistenceError(ctx, err)
			}
			return setup.ExchangeResult{Outcome: setup.ExchangeRateLimited, RetryAfter: retryAfter}, nil
		}
		var material setup.ExchangeMaterial
		if peerCount == peerFailureLimit || globalCount == globalFailureLimit {
			material, err = exchangeMaterial(request)
			if err != nil {
				return result, err
			}
		}
		if peerCount == peerFailureLimit {
			if err = insertRateLimitEvent(ctx, tx, material.PeerEventID, setup.RateScopePeer); err != nil {
				return result, classifySetupPersistenceError(ctx, err)
			}
		}
		if globalCount == globalFailureLimit {
			if err = insertRateLimitEvent(ctx, tx, material.GlobalEventID, setup.RateScopeGlobal); err != nil {
				return result, classifySetupPersistenceError(ctx, err)
			}
		}
		if err = tx.Commit(ctx); err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		return setup.ExchangeResult{Outcome: setup.ExchangeTokenInvalid}, nil
	}
	material, err := exchangeMaterial(request)
	if err != nil {
		return result, err
	}

	var createdAt, expiresAt time.Time
	err = tx.QueryRow(ctx, `INSERT INTO setup_sessions (id, token_digest, created_at, expires_at)
		VALUES ($1, $2, transaction_timestamp(), transaction_timestamp() + interval '15 minutes')
		RETURNING created_at, expires_at`, material.SessionID, material.SessionDigest[:]).Scan(&createdAt, &expiresAt)
	if err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM setup_exchange_rate_limits
		WHERE scope = 'peer' AND key_digest = $1 AND window_start = $2`, request.PeerKey[:], windowStart); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if err = insertSessionEvent(ctx, tx, material.EventID, "session_issued", material.SessionID, nil); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	return setup.ExchangeResult{Outcome: setup.ExchangeSessionCreated, SessionID: material.SessionID, ExpiresAt: expiresAt}, nil
}

func exchangeMaterial(request setup.ExchangeRequest) (setup.ExchangeMaterial, error) {
	if request.Material != nil {
		material, err := request.Material()
		if err != nil {
			return setup.ExchangeMaterial{}, setup.ErrPersistenceUnavailable
		}
		return material, nil
	}
	return setup.ExchangeMaterial{
		SessionID: request.SessionID, SessionDigest: request.SessionDigest, EventID: request.EventID,
		PeerEventID: request.PeerEventID, GlobalEventID: request.GlobalEventID,
	}, nil
}

func (r *SetupBootstrapRepository) ValidateSession(ctx context.Context, digest setup.Digest) (setup.Session, error) {
	if err := ctx.Err(); err != nil {
		return setup.Session{}, setup.ErrPersistenceCanceled
	}
	if !r.available() {
		return setup.Session{}, setup.ErrPersistenceUnavailable
	}
	var (
		statePresent bool
		complete     bool
		id           *setup.PersistenceID
		createdAt    *time.Time
		expiresAt    *time.Time
	)
	err := r.pool.pool.QueryRow(ctx, `SELECT st.singleton IS NOT NULL, COALESCE(st.complete, FALSE), s.id, s.created_at, s.expires_at
		FROM (VALUES (TRUE)) AS expected(singleton)
		LEFT JOIN setup_state st ON st.singleton = expected.singleton
		LEFT JOIN setup_sessions s ON st.complete = FALSE
		  AND s.token_digest = $1 AND s.revoked_at IS NULL
		  AND s.expires_at > transaction_timestamp()`, digest[:]).Scan(&statePresent, &complete, &id, &createdAt, &expiresAt)
	if err != nil {
		return setup.Session{}, classifySetupPersistenceError(ctx, err)
	}
	if !statePresent {
		return setup.Session{}, setup.ErrPersistenceUnavailable
	}
	if complete || id == nil || createdAt == nil || expiresAt == nil {
		return setup.Session{}, setup.ErrInvalidPersistenceInput
	}
	return setup.Session{ID: *id, CreatedAt: *createdAt, ExpiresAt: *expiresAt}, nil
}

func (r *SetupBootstrapRepository) RevokeSession(ctx context.Context, id, eventID setup.PersistenceID) (bool, error) {
	if !r.available() {
		return false, setup.ErrPersistenceUnavailable
	}
	tx, err := r.pool.pool.Begin(ctx)
	if err != nil {
		return false, classifySetupPersistenceError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var revoked bool
	err = tx.QueryRow(ctx, `UPDATE setup_sessions SET revoked_at = transaction_timestamp()
		WHERE id = $1 AND revoked_at IS NULL RETURNING TRUE`, id).Scan(&revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return false, classifySetupPersistenceError(ctx, err)
		}
		return false, nil
	}
	if err != nil {
		return false, classifySetupPersistenceError(ctx, err)
	}
	if err = insertSessionEvent(ctx, tx, eventID, "session_revoked", id, nil); err != nil {
		return false, classifySetupPersistenceError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return false, classifySetupPersistenceError(ctx, err)
	}
	return true, nil
}

func (r *SetupBootstrapRepository) Complete(ctx context.Context, request setup.CompletionRequest) (setup.CompletionResult, error) {
	if !r.available() {
		return setup.CompletionResult{}, setup.ErrPersistenceUnavailable
	}
	tx, err := r.pool.pool.Begin(ctx)
	if err != nil {
		return setup.CompletionResult{}, classifySetupPersistenceError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var complete bool
	if err = tx.QueryRow(ctx, `SELECT complete FROM setup_state WHERE singleton = TRUE FOR UPDATE`).Scan(&complete); err != nil {
		return setup.CompletionResult{}, classifySetupPersistenceError(ctx, err)
	}
	if complete {
		return setup.CompletionResult{}, setup.ErrSetupAlreadyComplete
	}
	if _, err = tx.Exec(ctx, `UPDATE setup_state SET complete = TRUE, updated_at = transaction_timestamp() WHERE singleton = TRUE`); err != nil {
		return setup.CompletionResult{}, classifySetupPersistenceError(ctx, err)
	}
	command, err := tx.Exec(ctx, `UPDATE setup_sessions SET revoked_at = transaction_timestamp()
		WHERE revoked_at IS NULL AND expires_at > transaction_timestamp()`)
	if err != nil {
		return setup.CompletionResult{}, classifySetupPersistenceError(ctx, err)
	}
	count := command.RowsAffected()
	if err = insertEvent(ctx, tx, request.RevocationEventID, "sessions_revoked_on_completion", nil, struct {
		AffectedSessions int64 `json:"affected_session_count"`
	}{count}); err != nil {
		return setup.CompletionResult{}, classifySetupPersistenceError(ctx, err)
	}
	if err = insertEvent(ctx, tx, request.CompletionEventID, "setup_completed", nil, struct{}{}); err != nil {
		return setup.CompletionResult{}, classifySetupPersistenceError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return setup.CompletionResult{}, classifySetupPersistenceError(ctx, err)
	}
	return setup.CompletionResult{RevokedSessions: count}, nil
}

func (r *SetupBootstrapRepository) Cleanup(ctx context.Context) (setup.CleanupResult, error) {
	if !r.available() {
		return setup.CleanupResult{}, setup.ErrPersistenceUnavailable
	}
	var result setup.CleanupResult
	err := r.pool.pool.QueryRow(ctx, `WITH doomed AS (
		SELECT ctid FROM setup_exchange_rate_limits
		WHERE window_start < transaction_timestamp() - interval '24 hours'
		ORDER BY window_start LIMIT $1
	), deleted AS (DELETE FROM setup_exchange_rate_limits WHERE ctid IN (SELECT ctid FROM doomed) RETURNING 1)
	SELECT count(*) FROM deleted`, cleanupLimit).Scan(&result.RateLimits)
	if err != nil {
		return setup.CleanupResult{}, classifySetupPersistenceError(ctx, err)
	}
	err = r.pool.pool.QueryRow(ctx, `WITH doomed AS (
		SELECT ctid FROM setup_sessions
		WHERE (expires_at < transaction_timestamp() - interval '24 hours')
		   OR (revoked_at < transaction_timestamp() - interval '24 hours')
		ORDER BY expires_at LIMIT $1
	), deleted AS (DELETE FROM setup_sessions WHERE ctid IN (SELECT ctid FROM doomed) RETURNING 1)
	SELECT count(*) FROM deleted`, cleanupLimit).Scan(&result.Sessions)
	if err != nil {
		return setup.CleanupResult{}, classifySetupPersistenceError(ctx, err)
	}
	return result, nil
}

func (r *SetupBootstrapRepository) available() bool {
	return r != nil && r.pool != nil && r.pool.pool != nil
}

func exchangeWindow(ctx context.Context, tx pgx.Tx) (time.Time, time.Duration, error) {
	var start time.Time
	var retrySeconds int64
	err := tx.QueryRow(ctx, `WITH boundary AS (
		SELECT date_bin(interval '15 minutes', transaction_timestamp(), timestamptz '2000-01-01 00:00:00+00') AS window_start
	) SELECT window_start, greatest(1, least(900, ceil(extract(epoch FROM window_start + interval '15 minutes' - transaction_timestamp()))::bigint))
	FROM boundary`).Scan(&start, &retrySeconds)
	return start, time.Duration(retrySeconds) * time.Second, err
}

func anyRateLimitExhausted(ctx context.Context, tx pgx.Tx, request setup.ExchangeRequest, window time.Time) (bool, error) {
	var exhausted bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM setup_exchange_rate_limits
		WHERE window_start = $1 AND ((scope = 'peer' AND key_digest = $2 AND failure_count >= $4)
		 OR (scope = 'global' AND key_digest = $3 AND failure_count >= $5)))`,
		window, request.PeerKey[:], request.GlobalKey[:], peerFailureLimit, globalFailureLimit).Scan(&exhausted)
	return exhausted, err
}

func incrementRateLimit(ctx context.Context, tx pgx.Tx, scope setup.RateScope, key setup.Digest, window time.Time, limit int32) (int32, bool, error) {
	var count int32
	err := tx.QueryRow(ctx, `INSERT INTO setup_exchange_rate_limits (scope, key_digest, window_start, failure_count, updated_at)
		VALUES ($1, $2, $3, 1, transaction_timestamp())
		ON CONFLICT (scope, key_digest, window_start) DO UPDATE
		SET failure_count = setup_exchange_rate_limits.failure_count + 1, updated_at = transaction_timestamp()
		WHERE setup_exchange_rate_limits.failure_count < $4
		RETURNING failure_count`, scope, key[:], window, limit).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, true, nil
	}
	return count, false, err
}

func insertRateLimitEvent(ctx context.Context, tx pgx.Tx, id setup.PersistenceID, scope setup.RateScope) error {
	return insertEvent(ctx, tx, id, "rate_limit_activated", nil, struct {
		Scope setup.RateScope `json:"scope"`
	}{scope})
}

func insertSessionEvent(ctx context.Context, tx pgx.Tx, id setup.PersistenceID, kind string, sessionID setup.PersistenceID, data any) error {
	return insertEvent(ctx, tx, id, kind, &sessionID, data)
}

func insertEvent(ctx context.Context, tx pgx.Tx, id setup.PersistenceID, kind string, sessionID *setup.PersistenceID, data any) error {
	if !allowedSetupEvent(kind) {
		return setup.ErrInvalidPersistenceInput
	}
	if data == nil {
		data = struct{}{}
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return setup.ErrInvalidPersistenceInput
	}
	var databaseSessionID any
	if sessionID != nil {
		databaseSessionID = *sessionID
	}
	_, err = tx.Exec(ctx, `INSERT INTO setup_security_events (id, event_type, session_id, data, created_at)
		VALUES ($1, $2, $3, $4, transaction_timestamp())`, id, kind, databaseSessionID, payload)
	return err
}

func allowedSetupEvent(kind string) bool {
	switch kind {
	case "session_issued", "session_revoked", "sessions_revoked_on_completion", "rate_limit_activated", "setup_completed":
		return true
	default:
		return false
	}
}

func classifySetupPersistenceError(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return setup.ErrPersistenceCanceled
	}
	if errors.Is(err, setup.ErrInvalidPersistenceInput) {
		return err
	}
	return setup.ErrPersistenceUnavailable
}
