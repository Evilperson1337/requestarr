//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"git.example.com/evilperson1337/Requestarr/internal/setup"
	storemigrations "git.example.com/evilperson1337/Requestarr/internal/store/postgres/migrations"
)

func TestSetupExchangeSessionValidationRevocationAndAudit(t *testing.T) {
	repository, pool := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	request := validExchangeRequest(1)
	result, err := repository.Exchange(ctx, request)
	if err != nil || result.Outcome != setup.ExchangeSessionCreated || result.ExpiresAt.IsZero() {
		t.Fatalf("Exchange() = %+v, %v", result, err)
	}
	if _, err := repository.ValidateSession(ctx, request.SessionDigest); err != nil {
		t.Fatalf("ValidateSession(valid) = %v", err)
	}
	var eventType string
	var eventData string
	if err := pool.QueryRow(ctx, `SELECT event_type, data::text FROM setup_security_events WHERE session_id=$1`, request.SessionID).Scan(&eventType, &eventData); err != nil {
		t.Fatalf("read session audit: %v", err)
	}
	if eventType != "session_issued" || eventData != "{}" {
		t.Fatalf("session audit = %q %s", eventType, eventData)
	}

	revoked, err := repository.RevokeSession(ctx, request.SessionID, persistenceID(20))
	if err != nil || !revoked {
		t.Fatalf("RevokeSession(first) = %t, %v", revoked, err)
	}
	revoked, err = repository.RevokeSession(ctx, request.SessionID, persistenceID(21))
	if err != nil || revoked {
		t.Fatalf("RevokeSession(second) = %t, %v", revoked, err)
	}
	if _, err := repository.ValidateSession(ctx, request.SessionDigest); !errors.Is(err, setup.ErrInvalidPersistenceInput) {
		t.Fatalf("ValidateSession(revoked) = %v", err)
	}

	expired := validExchangeRequest(2)
	if _, err := repository.Exchange(ctx, expired); err != nil {
		t.Fatalf("create session for expiry test: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE setup_sessions SET created_at=transaction_timestamp()-interval '16 minutes', expires_at=transaction_timestamp()-interval '1 minute' WHERE id=$1`, expired.SessionID); err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if _, err := repository.ValidateSession(ctx, expired.SessionDigest); !errors.Is(err, setup.ErrInvalidPersistenceInput) {
		t.Fatalf("ValidateSession(expired) = %v", err)
	}
}

func TestSetupSessionValidationDistinguishesMissingSingleton(t *testing.T) {
	repository, pool := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	request := validExchangeRequest(9)
	if _, err := repository.Exchange(ctx, request); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM setup_state WHERE singleton=TRUE`); err != nil {
		t.Fatalf("remove setup singleton: %v", err)
	}
	if _, err := repository.ValidateSession(ctx, request.SessionDigest); !errors.Is(err, setup.ErrPersistenceUnavailable) {
		t.Fatalf("ValidateSession() error = %v, want unavailable for invalid persisted state", err)
	}
}

func TestSetupRepositoryCancellationAndClosedPoolAreSecretSafe(t *testing.T) {
	repository, _ := isolatedSetupRepository(t)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	request := validExchangeRequest(12)

	if _, err := repository.Exchange(canceled, request); !errors.Is(err, setup.ErrPersistenceCanceled) {
		t.Fatalf("Exchange(canceled) = %v", err)
	}
	if _, err := repository.ValidateSession(canceled, request.SessionDigest); !errors.Is(err, setup.ErrPersistenceCanceled) {
		t.Fatalf("ValidateSession(canceled) = %v", err)
	}

	repository.pool.Close()
	secretPattern := regexp.MustCompile(`(?i)postgres|requestarr_test_|[0-9a-f]{64}`)
	operations := []struct {
		name string
		run  func() error
	}{
		{name: "exchange", run: func() error { _, err := repository.Exchange(context.Background(), request); return err }},
		{name: "validate", run: func() error {
			_, err := repository.ValidateSession(context.Background(), request.SessionDigest)
			return err
		}},
		{name: "revoke", run: func() error {
			_, err := repository.RevokeSession(context.Background(), request.SessionID, request.EventID)
			return err
		}},
		{name: "complete", run: func() error {
			_, err := repository.Complete(context.Background(), setup.CompletionRequest{})
			return err
		}},
		{name: "cleanup", run: func() error { _, err := repository.Cleanup(context.Background()); return err }},
	}
	for _, operation := range operations {
		err := operation.run()
		if !errors.Is(err, setup.ErrPersistenceUnavailable) {
			t.Errorf("%s(closed pool) = %v, want unavailable", operation.name, err)
		} else if secretPattern.MatchString(err.Error()) {
			t.Errorf("%s(closed pool) exposed persistence details: %v", operation.name, err)
		}
	}
}

func TestSetupConcurrentValidExchangesCreateIndependentSessions(t *testing.T) {
	repository, pool := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const exchanges = 8
	start := make(chan struct{})
	errs := make(chan error, exchanges)
	var workers sync.WaitGroup
	for i := range exchanges {
		request := validExchangeRequest(byte(30 + i))
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, err := repository.Exchange(ctx, request)
			if err == nil && result.Outcome != setup.ExchangeSessionCreated {
				err = errors.New("valid exchange did not create a session")
			}
			errs <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent valid exchange: %v", err)
		}
	}
	var sessions, events int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM setup_sessions),
		(SELECT count(*) FROM setup_security_events WHERE event_type='session_issued')`).Scan(&sessions, &events); err != nil {
		t.Fatalf("read concurrent exchange effects: %v", err)
	}
	if sessions != exchanges || events != exchanges {
		t.Fatalf("concurrent exchange effects sessions=%d events=%d, want %d each", sessions, events, exchanges)
	}
}

func TestSetupInvalidAttemptAtomicityAndActivationAudit(t *testing.T) {
	repository, pool := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	request := validExchangeRequest(3)
	request.TokenValid = false
	const attempts = 5
	start := make(chan struct{})
	results := make(chan setup.ExchangeOutcome, attempts)
	errs := make(chan error, attempts)
	var workers sync.WaitGroup
	for range attempts {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, err := repository.Exchange(ctx, request)
			results <- result.Outcome
			errs <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent invalid exchange: %v", err)
		}
	}
	for outcome := range results {
		if outcome != setup.ExchangeTokenInvalid {
			t.Fatalf("outcome = %v, want invalid token", outcome)
		}
	}
	var peerCount, globalCount int
	if err := pool.QueryRow(ctx, `SELECT max(failure_count) FILTER (WHERE scope='peer'), max(failure_count) FILTER (WHERE scope='global') FROM setup_exchange_rate_limits`).Scan(&peerCount, &globalCount); err != nil {
		t.Fatalf("read atomic counters: %v", err)
	}
	if peerCount != attempts || globalCount != attempts {
		t.Fatalf("counts peer=%d global=%d, want %d", peerCount, globalCount, attempts)
	}
	var peerEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM setup_security_events WHERE event_type='rate_limit_activated' AND data->>'scope'='peer'`).Scan(&peerEvents); err != nil || peerEvents != 1 {
		t.Fatalf("peer activation events=%d err=%v", peerEvents, err)
	}
	result, err := repository.Exchange(ctx, request)
	if err != nil || result.Outcome != setup.ExchangeRateLimited {
		t.Fatalf("exhausted Exchange() = %+v, %v", result, err)
	}
}

func TestSetupExhaustedLimitDoesNotVerifyCandidate(t *testing.T) {
	repository, _ := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	request := validExchangeRequest(13)
	request.TokenValid = false
	for range 5 {
		if result, err := repository.Exchange(ctx, request); err != nil || result.Outcome != setup.ExchangeTokenInvalid {
			t.Fatalf("seed invalid attempt = %+v, %v", result, err)
		}
	}
	request.Verify = func() bool {
		t.Fatal("candidate verifier called after durable limit was exhausted")
		return true
	}
	result, err := repository.Exchange(ctx, request)
	if err != nil || result.Outcome != setup.ExchangeRateLimited {
		t.Fatalf("exhausted Exchange() = %+v, %v", result, err)
	}
}

func TestSetupGlobalRateLimitAndActivationAudit(t *testing.T) {
	repository, pool := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := range 100 {
		request := validExchangeRequest(byte(i))
		request.TokenValid = false
		request.GlobalEventID = persistenceID(240)
		result, err := repository.Exchange(ctx, request)
		if err != nil || result.Outcome != setup.ExchangeTokenInvalid {
			t.Fatalf("invalid exchange %d = %+v, %v", i+1, result, err)
		}
	}
	limited := validExchangeRequest(101)
	limited.TokenValid = false
	result, err := repository.Exchange(ctx, limited)
	if err != nil || result.Outcome != setup.ExchangeRateLimited || result.RetryAfter < time.Second || result.RetryAfter > 15*time.Minute {
		t.Fatalf("globally limited Exchange() = %+v, %v", result, err)
	}
	var count, events int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT failure_count FROM setup_exchange_rate_limits WHERE scope='global'),
		(SELECT count(*) FROM setup_security_events WHERE event_type='rate_limit_activated' AND data = '{"scope":"global"}'::jsonb)`).Scan(&count, &events); err != nil {
		t.Fatalf("read global limit effects: %v", err)
	}
	if count != 100 || events != 1 {
		t.Fatalf("global limit effects count=%d events=%d, want 100 and 1", count, events)
	}
}

func TestSetupInvalidAttemptRollsBackBothCountersWhenAuditFails(t *testing.T) {
	repository, pool := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	request := validExchangeRequest(10)
	request.TokenValid = false
	for range 4 {
		if result, err := repository.Exchange(ctx, request); err != nil || result.Outcome != setup.ExchangeTokenInvalid {
			t.Fatalf("seed invalid attempt = %+v, %v", result, err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO setup_security_events (id,event_type,created_at)
		VALUES ($1,'setup_completed',transaction_timestamp())`, request.PeerEventID); err != nil {
		t.Fatalf("seed activation audit collision: %v", err)
	}
	if _, err := repository.Exchange(ctx, request); !errors.Is(err, setup.ErrPersistenceUnavailable) {
		t.Fatalf("activation audit failure = %v, want unavailable", err)
	}
	var peerCount, globalCount int
	if err := pool.QueryRow(ctx, `SELECT max(failure_count) FILTER (WHERE scope='peer'),
		max(failure_count) FILTER (WHERE scope='global') FROM setup_exchange_rate_limits`).Scan(&peerCount, &globalCount); err != nil {
		t.Fatalf("read rolled-back counters: %v", err)
	}
	if peerCount != 4 || globalCount != 4 {
		t.Fatalf("counters after rollback peer=%d global=%d, want 4 each", peerCount, globalCount)
	}
}

func TestSetupCompletionSerializesAndRevokes(t *testing.T) {
	repository, pool := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	issued := validExchangeRequest(4)
	if _, err := repository.Exchange(ctx, issued); err != nil {
		t.Fatalf("issue before completion: %v", err)
	}
	result, err := repository.Complete(ctx, setup.CompletionRequest{RevocationEventID: persistenceID(40), CompletionEventID: persistenceID(41)})
	if err != nil || result.RevokedSessions != 1 {
		t.Fatalf("Complete() = %+v, %v", result, err)
	}
	if _, err := repository.ValidateSession(ctx, issued.SessionDigest); !errors.Is(err, setup.ErrInvalidPersistenceInput) {
		t.Fatalf("completed session validation = %v", err)
	}
	after := validExchangeRequest(5)
	exchange, err := repository.Exchange(ctx, after)
	if err != nil || exchange.Outcome != setup.ExchangeSetupCompleted {
		t.Fatalf("exchange after completion = %+v, %v", exchange, err)
	}
	var sessions, events int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM setup_sessions), (SELECT count(*) FROM setup_security_events WHERE event_type IN ('sessions_revoked_on_completion','setup_completed'))`).Scan(&sessions, &events); err != nil {
		t.Fatalf("read completion effects: %v", err)
	}
	if sessions != 1 || events != 2 {
		t.Fatalf("completion effects sessions=%d events=%d", sessions, events)
	}
	if _, err := repository.Complete(ctx, setup.CompletionRequest{RevocationEventID: persistenceID(42), CompletionEventID: persistenceID(43)}); !errors.Is(err, setup.ErrSetupAlreadyComplete) {
		t.Fatalf("second Complete() = %v", err)
	}
}

func TestSetupCompletionLockPreventsConcurrentSessionCreation(t *testing.T) {
	repository, pool := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin completion lock: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SELECT complete FROM setup_state WHERE singleton=TRUE FOR UPDATE`); err != nil {
		t.Fatalf("lock setup singleton: %v", err)
	}

	resultChannel := make(chan setup.ExchangeResult, 1)
	errorChannel := make(chan error, 1)
	go func() {
		result, err := repository.Exchange(ctx, validExchangeRequest(8))
		resultChannel <- result
		errorChannel <- err
	}()
	select {
	case <-resultChannel:
		t.Fatal("exchange did not wait for setup completion lock")
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := tx.Exec(ctx, `UPDATE setup_state SET complete=TRUE, updated_at=transaction_timestamp() WHERE singleton=TRUE`); err != nil {
		t.Fatalf("complete while holding lock: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit completion lock: %v", err)
	}
	result := <-resultChannel
	if err := <-errorChannel; err != nil || result.Outcome != setup.ExchangeSetupCompleted {
		t.Fatalf("blocked exchange = %+v, %v", result, err)
	}
	var sessions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM setup_sessions`).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("sessions after completion won lock=%d err=%v", sessions, err)
	}
}

func TestSetupExchangeRollsBackSessionWhenAuditFails(t *testing.T) {
	repository, pool := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request := validExchangeRequest(6)
	request.EventID = setup.PersistenceID{}
	if _, err := pool.Exec(ctx, `INSERT INTO setup_security_events (id,event_type,created_at) VALUES ($1,'setup_completed',transaction_timestamp())`, request.EventID); err != nil {
		t.Fatalf("seed audit collision: %v", err)
	}
	if _, err := repository.Exchange(ctx, request); !errors.Is(err, setup.ErrPersistenceUnavailable) {
		t.Fatalf("Exchange() error = %v", err)
	}
	var sessions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM setup_sessions WHERE id=$1`, request.SessionID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("rolled-back sessions=%d err=%v", sessions, err)
	}
}

func TestSetupCleanupIsBoundedAndPreservesActiveRows(t *testing.T) {
	repository, pool := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	active := validExchangeRequest(7)
	if _, err := repository.Exchange(ctx, active); err != nil {
		t.Fatalf("create active session: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO setup_exchange_rate_limits (key_digest,scope,window_start,failure_count,updated_at)
		SELECT decode(lpad(to_hex(i),64,'0'),'hex'),'peer',transaction_timestamp()-interval '25 hours',1,transaction_timestamp()-interval '25 hours'
		FROM generate_series(1,1001) i`); err != nil {
		t.Fatalf("seed stale rates: %v", err)
	}
	result, err := repository.Cleanup(ctx)
	if err != nil || result.RateLimits != 1000 || result.Sessions != 0 {
		t.Fatalf("Cleanup() = %+v, %v", result, err)
	}
	if _, err := repository.ValidateSession(ctx, active.SessionDigest); err != nil {
		t.Fatalf("cleanup removed active session: %v", err)
	}
}

func TestSetupCleanupBoundsSessionsAndProtectsRecentRevocations(t *testing.T) {
	repository, pool := isolatedSetupRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := pool.Exec(ctx, `INSERT INTO setup_sessions (id,token_digest,created_at,expires_at)
		SELECT gen_random_uuid(), decode(md5(i::text) || md5((i+10000)::text), 'hex'),
			transaction_timestamp()-interval '26 hours', transaction_timestamp()-interval '25 hours'
		FROM generate_series(1,1001) i`); err != nil {
		t.Fatalf("seed stale sessions: %v", err)
	}
	recent := validExchangeRequest(11)
	if _, err := repository.Exchange(ctx, recent); err != nil {
		t.Fatalf("create recent session: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE setup_sessions SET revoked_at=transaction_timestamp() WHERE id=$1`, recent.SessionID); err != nil {
		t.Fatalf("recently revoke session: %v", err)
	}
	result, err := repository.Cleanup(ctx)
	if err != nil || result.Sessions != 1000 {
		t.Fatalf("Cleanup() = %+v, %v", result, err)
	}
	var recentRows, staleRows int
	if err := pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE id=$1),
		count(*) FILTER (WHERE expires_at < transaction_timestamp()-interval '24 hours')
		FROM setup_sessions`, recent.SessionID).Scan(&recentRows, &staleRows); err != nil {
		t.Fatalf("read cleanup survivors: %v", err)
	}
	if recentRows != 1 || staleRows != 1 {
		t.Fatalf("cleanup survivors recent=%d stale=%d, want 1 each", recentRows, staleRows)
	}
}

func TestSetupWorkflowGeneralIsResumableIdempotentAndMonotonic(t *testing.T) {
	bootstrap, pool := isolatedSetupRepository(t)
	repository := NewSetupWorkflowRepository(bootstrap.pool)
	ctx := context.Background()
	sessionDigest := digest(211)
	_, err := pool.Exec(ctx, `INSERT INTO setup_sessions (id, token_digest, created_at, expires_at)
		VALUES ($1, $2, transaction_timestamp(), transaction_timestamp() + interval '15 minutes')`, persistenceID(211), sessionDigest[:])
	if err != nil {
		t.Fatal(err)
	}
	general := setup.General{InstanceName: "Requestarr", PublicURL: "https://example.com", Timezone: "UTC"}
	for range 2 {
		result, err := repository.SaveGeneral(ctx, sessionDigest, general)
		if err != nil || result.Outcome != setup.WorkflowAvailable || result.State.Current != setup.StepAuthentication || result.State.General == nil || *result.State.General != general {
			t.Fatalf("unexpected save result: %#v, %v", result, err)
		}
	}
	var auditCount int
	var eventType string
	if err := pool.QueryRow(ctx, `SELECT count(*), min(event_type) FROM setup_workflow_events`).Scan(&auditCount, &eventType); err != nil || auditCount != 1 || eventType != "general_saved" {
		t.Fatalf("idempotent audit count=%d type=%q err=%v", auditCount, eventType, err)
	}
	var payloadColumns int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='setup_workflow_events'
		AND column_name NOT IN ('id','event_type','created_at')`).Scan(&payloadColumns); err != nil || payloadColumns != 0 {
		t.Fatalf("workflow audit payload columns=%d err=%v", payloadColumns, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE setup_state SET current_step = 'libraries' WHERE singleton = TRUE`); err != nil {
		t.Fatal(err)
	}
	result, err := repository.SaveGeneral(ctx, sessionDigest, general)
	if err != nil || result.State.Current != setup.StepLibraries {
		t.Fatalf("General save moved workflow backward: %#v, %v", result, err)
	}
	read, err := repository.ReadWorkflow(ctx, sessionDigest)
	if err != nil || read.Outcome != setup.WorkflowAvailable || read.State.Current != setup.StepLibraries || read.State.General == nil || *read.State.General != general {
		t.Fatalf("resumable read failed: %#v, %v", read, err)
	}

	restarted := NewSetupWorkflowRepository(bootstrap.pool)
	read, err = restarted.ReadWorkflow(ctx, sessionDigest)
	if err != nil || read.State.General == nil || *read.State.General != general {
		t.Fatalf("restart resumability failed: %#v, %v", read, err)
	}
}

func TestSetupWorkflowSaveWaitsThenRejectsExpiredSessionWithoutWrite(t *testing.T) {
	bootstrap, pool := isolatedSetupRepository(t)
	repository := NewSetupWorkflowRepository(bootstrap.pool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	digest := digest(214)
	if _, err := pool.Exec(ctx, `INSERT INTO setup_sessions (id, token_digest, created_at, expires_at)
		VALUES ($1, $2, clock_timestamp(), clock_timestamp() + interval '300 milliseconds')`, persistenceID(214), digest[:]); err != nil {
		t.Fatal(err)
	}
	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback(context.Background()) }()
	if _, err := lock.Exec(ctx, `SELECT 1 FROM setup_state WHERE singleton=TRUE FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan setup.WorkflowResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := repository.SaveGeneral(ctx, digest, setup.General{InstanceName: "must-not-save", PublicURL: "https://example.com", Timezone: "UTC"})
		resultCh <- result
		errCh <- err
	}()
	select {
	case <-resultCh:
		t.Fatal("save did not wait for setup-state lock")
	case <-time.After(500 * time.Millisecond):
	}
	if err := lock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := <-resultCh
	if err := <-errCh; err != nil || result.Outcome != setup.WorkflowSessionRequired {
		t.Fatalf("blocked expired save = %#v, %v", result, err)
	}
	var draft string
	var events int
	if err := pool.QueryRow(ctx, `SELECT draft::text, (SELECT count(*) FROM setup_workflow_events) FROM setup_state WHERE singleton=TRUE`).Scan(&draft, &events); err != nil || draft != "{}" || events != 0 {
		t.Fatalf("expired save effects draft=%s events=%d err=%v", draft, events, err)
	}
}

func TestSetupWorkflowRevokedExpiredConcurrentAndCanceledSaves(t *testing.T) {
	bootstrap, pool := isolatedSetupRepository(t)
	repository := NewSetupWorkflowRepository(bootstrap.pool)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for seed, state := range map[byte]string{215: "revoked", 216: "expired"} {
		d := digest(seed)
		query := `INSERT INTO setup_sessions (id, token_digest, created_at, expires_at, revoked_at)
			VALUES ($1,$2,clock_timestamp()-interval '2 minutes',clock_timestamp()+interval '2 minutes',clock_timestamp())`
		if state == "expired" {
			query = `INSERT INTO setup_sessions (id, token_digest, created_at, expires_at)
				VALUES ($1,$2,clock_timestamp()-interval '2 minutes',clock_timestamp()-interval '1 minute')`
		}
		if _, err := pool.Exec(ctx, query, persistenceID(seed), d[:]); err != nil {
			t.Fatal(err)
		}
		if result, err := repository.SaveGeneral(ctx, d, setup.General{InstanceName: state, PublicURL: "https://example.com", Timezone: "UTC"}); err != nil || result.Outcome != setup.WorkflowSessionRequired {
			t.Fatalf("%s save = %#v, %v", state, result, err)
		}
	}
	active := digest(217)
	if _, err := pool.Exec(ctx, `INSERT INTO setup_sessions (id,token_digest,created_at,expires_at) VALUES ($1,$2,clock_timestamp(),clock_timestamp()+interval '2 minutes')`, persistenceID(217), active[:]); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 4)
	for range 4 {
		go func() {
			<-start
			result, err := repository.SaveGeneral(ctx, active, setup.General{InstanceName: "same", PublicURL: "https://example.com", Timezone: "UTC"})
			if err == nil && result.Outcome != setup.WorkflowAvailable {
				err = errors.New("unexpected save outcome")
			}
			errs <- err
		}()
	}
	close(start)
	for range 4 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM setup_workflow_events`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("concurrent identical save events=%d err=%v", events, err)
	}

	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Exec(ctx, `SELECT 1 FROM setup_state WHERE singleton=TRUE FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	waitCtx, stop := context.WithCancel(ctx)
	waitErr := make(chan error, 1)
	go func() {
		_, err := repository.SaveGeneral(waitCtx, active, setup.General{InstanceName: "canceled", PublicURL: "https://example.com", Timezone: "UTC"})
		waitErr <- err
	}()
	time.Sleep(100 * time.Millisecond)
	stop()
	if err := <-waitErr; !errors.Is(err, setup.ErrPersistenceCanceled) {
		t.Fatalf("canceled lock wait error=%v", err)
	}
	_ = lock.Rollback(ctx)
}

func TestSetupWorkflowSaveSerializesWithCompletion(t *testing.T) {
	bootstrap, pool := isolatedSetupRepository(t)
	workflow := NewSetupWorkflowRepository(bootstrap.pool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d := digest(218)
	if _, err := pool.Exec(ctx, `INSERT INTO setup_sessions (id,token_digest,created_at,expires_at) VALUES ($1,$2,clock_timestamp(),clock_timestamp()+interval '2 minutes')`, persistenceID(218), d[:]); err != nil {
		t.Fatal(err)
	}
	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Exec(ctx, `SELECT 1 FROM setup_state WHERE singleton=TRUE FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan setup.WorkflowResult, 1)
	go func() {
		result, _ := workflow.SaveGeneral(ctx, d, setup.General{InstanceName: "late", PublicURL: "https://example.com", Timezone: "UTC"})
		resultCh <- result
	}()
	time.Sleep(100 * time.Millisecond)
	if _, err := lock.Exec(ctx, `UPDATE setup_state SET complete=TRUE WHERE singleton=TRUE`); err != nil {
		t.Fatal(err)
	}
	if err := lock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if result := <-resultCh; result.Outcome != setup.WorkflowCompleted {
		t.Fatalf("save after serialized completion = %#v", result)
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM setup_workflow_events`).Scan(&events); err != nil || events != 0 {
		t.Fatalf("completion-losing save events=%d err=%v", events, err)
	}
}

func TestSetupWorkflowCompletedMutationPrecedesCompletionRevocation(t *testing.T) {
	bootstrap, pool := isolatedSetupRepository(t)
	workflow := NewSetupWorkflowRepository(bootstrap.pool)
	ctx := context.Background()
	d := digest(219)
	if _, err := pool.Exec(ctx, `INSERT INTO setup_sessions (id,token_digest,created_at,expires_at)
		VALUES ($1,$2,clock_timestamp(),clock_timestamp()+interval '2 minutes')`, persistenceID(219), d[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Complete(ctx, setup.CompletionRequest{
		RevocationEventID: persistenceID(220), CompletionEventID: persistenceID(221),
	}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}

	result, err := workflow.SaveGeneral(ctx, d, setup.General{
		InstanceName: "must-not-save", PublicURL: "https://example.com", Timezone: "UTC",
	})
	if err != nil || result.Outcome != setup.WorkflowCompleted {
		t.Fatalf("save with completion-revoked session = %#v, %v", result, err)
	}
	read, err := workflow.ReadWorkflow(ctx, d)
	if err != nil || read.Outcome != setup.WorkflowSessionRequired {
		t.Fatalf("read with completion-revoked session = %#v, %v", read, err)
	}
	var draft string
	var workflowEvents int
	var activeSessions int
	if err := pool.QueryRow(ctx, `SELECT draft::text,
		(SELECT count(*) FROM setup_workflow_events),
		(SELECT count(*) FROM setup_sessions WHERE revoked_at IS NULL)
		FROM setup_state WHERE singleton=TRUE`).Scan(&draft, &workflowEvents, &activeSessions); err != nil {
		t.Fatal(err)
	}
	if draft != "{}" || workflowEvents != 0 || activeSessions != 0 {
		t.Fatalf("completed save effects draft=%s workflow_events=%d active_sessions=%d", draft, workflowEvents, activeSessions)
	}
}

func TestSetupWorkflowAuthorizationCompletionAndStrictDraft(t *testing.T) {
	bootstrap, pool := isolatedSetupRepository(t)
	repository := NewSetupWorkflowRepository(bootstrap.pool)
	ctx := context.Background()
	valid := digest(212)
	_, err := pool.Exec(ctx, `INSERT INTO setup_sessions (id, token_digest, created_at, expires_at)
		VALUES ($1, $2, transaction_timestamp(), transaction_timestamp() + interval '15 minutes')`, persistenceID(212), valid[:])
	if err != nil {
		t.Fatal(err)
	}
	if result, err := repository.ReadWorkflow(ctx, digest(213)); err != nil || result.Outcome != setup.WorkflowSessionRequired {
		t.Fatalf("unknown session outcome = %#v, %v", result, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE setup_state SET draft = '{"version":1,"secret":"must-not-survive"}'::jsonb WHERE singleton = TRUE`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReadWorkflow(ctx, valid); !errors.Is(err, setup.ErrStateInvalid) {
		t.Fatalf("strict draft error = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE setup_state SET complete = TRUE WHERE singleton = TRUE`); err != nil {
		t.Fatal(err)
	}
	if result, err := repository.ReadWorkflow(ctx, digest(213)); err != nil || result.Outcome != setup.WorkflowSessionRequired {
		t.Fatalf("completed state disclosed without valid session: %#v, %v", result, err)
	}
	if result, err := repository.SaveGeneral(ctx, valid, setup.General{InstanceName: "x", PublicURL: "https://example.com", Timezone: "UTC"}); err != nil || result.Outcome != setup.WorkflowCompleted {
		t.Fatalf("completed mutation outcome = %#v, %v", result, err)
	}
	var draft string
	if err := pool.QueryRow(ctx, `SELECT draft::text FROM setup_state WHERE singleton = TRUE`).Scan(&draft); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(draft, "instance_name") {
		t.Fatalf("completed mutation changed draft: %s", draft)
	}
}

func validExchangeRequest(seed byte) setup.ExchangeRequest {
	return setup.ExchangeRequest{
		PeerKey:       digest(seed),
		GlobalKey:     digest(200),
		TokenValid:    true,
		SessionID:     persistenceID(seed),
		SessionDigest: digest(seed + 50),
		EventID:       persistenceID(seed + 80),
		PeerEventID:   persistenceID(seed + 100),
		GlobalEventID: persistenceID(seed + 120),
	}
}

func digest(seed byte) (value setup.Digest) {
	for i := range value {
		value[i] = seed + byte(i)
	}
	return value
}

func persistenceID(seed byte) (value setup.PersistenceID) {
	for i := range value {
		value[i] = seed + byte(i)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return value
}

func isolatedSetupRepository(t *testing.T) (*SetupBootstrapRepository, *pgxpool.Pool) {
	t.Helper()
	baseURL := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL is required for integration tests")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("REQUESTARR_TEST_DATABASE_URL must be an absolute PostgreSQL URL")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate schema: %v", err)
	}
	schema := "requestarr_test_" + hex.EncodeToString(random)
	if !setupTestSchemaPattern.MatchString(schema) {
		t.Fatal("generated schema failed safety validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal("open explicit test database")
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatalf("create schema: %v", err)
	}
	verified := false
	t.Cleanup(func() {
		defer admin.Close()
		if !verified || !setupTestSchemaPattern.MatchString(schema) {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	isolateURL := parsed.String()
	if err := storemigrations.Up(ctx, isolateURL); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, isolateURL)
	if err != nil {
		t.Fatal("open isolated schema")
	}
	t.Cleanup(pool.Close)
	var current string
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&current); err != nil || current != schema {
		t.Fatalf("verify isolated schema=%q err=%v", current, err)
	}
	verified = true
	return NewSetupBootstrapRepository(&Pool{pool: pool}), pool
}
