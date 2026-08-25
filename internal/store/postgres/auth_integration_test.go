//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
)

func TestAuthRepositoryConsumesTransactionsAndManagesLiveSessions(t *testing.T) {
	databaseURL, cleanup := encryptedTestSchema(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := migratedEncryptedPool(t, ctx, databaseURL)
	defer pool.Close()
	repository := NewAuthRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)

	transaction := auth.LoginTransaction{ID: authTestID(160), StateDigest: auth.Digest("state", "transaction"), NonceDigest: auth.Digest("nonce", "transaction"), PKCEVerifierDigest: auth.Digest("pkce", "transaction"), AuthenticationRevision: 1, CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
	if err := repository.CreateLoginTransaction(ctx, transaction); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := repository.ConsumeLoginTransaction(ctx, transaction.StateDigest, now)
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, auth.ErrLoginInvalid) {
			t.Fatalf("consume error=%v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful one-time consumptions=%d", successes)
	}
	expiredTransaction := auth.LoginTransaction{ID: authTestID(167), StateDigest: auth.Digest("state", "expired-transaction"), NonceDigest: auth.Digest("nonce", "expired-transaction"), PKCEVerifierDigest: auth.Digest("pkce", "expired-transaction"), AuthenticationRevision: 1, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Minute)}
	if err := repository.CreateLoginTransaction(ctx, expiredTransaction); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ConsumeLoginTransaction(ctx, expiredTransaction.StateDigest, now.Add(-24*time.Hour)); !errors.Is(err, auth.ErrLoginInvalid) {
		t.Fatalf("expired transaction accepted with caller-controlled time: %v", err)
	}

	first, err := repository.UpsertUser(ctx, auth.User{ID: authTestID(161), Issuer: "https://issuer-one.example", Subject: "same-subject", Email: "first@example.test", DisplayName: "First", Role: auth.RoleViewer}, now)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repository.UpsertUser(ctx, auth.User{ID: authTestID(162), Issuer: "https://issuer-one.example", Subject: "same-subject", Email: "second@example.test", DisplayName: "Second", Role: auth.RoleAdmin}, now.Add(time.Second))
	if err != nil || updated.ID != first.ID || updated.Email != "second@example.test" || updated.DisplayName != "Second" || updated.Role != auth.RoleAdmin {
		t.Fatalf("updated user=%+v err=%v", updated, err)
	}
	otherIssuer, err := repository.UpsertUser(ctx, auth.User{ID: authTestID(163), Issuer: "https://issuer-two.example", Subject: "same-subject", Email: "other@example.test", DisplayName: "Other", Role: auth.RoleRequester}, now)
	if err != nil || otherIssuer.ID == first.ID {
		t.Fatalf("issuer-scoped user=%+v err=%v", otherIssuer, err)
	}

	live := auth.Session{ID: authTestID(164), TokenDigest: auth.Digest("session", "live"), UserID: first.ID, AuthenticationRevision: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := repository.CreateSession(ctx, live); err != nil {
		t.Fatal(err)
	}
	if got, err := repository.ReadSession(ctx, live.TokenDigest, now); err != nil || got.ID != live.ID || got.UserID != first.ID {
		t.Fatalf("live session=%+v err=%v", got, err)
	}
	if err := repository.RevokeSession(ctx, live.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReadSession(ctx, live.TokenDigest, now.Add(2*time.Second)); !errors.Is(err, auth.ErrSessionRequired) {
		t.Fatalf("revoked session error=%v", err)
	}
	expired := auth.Session{ID: authTestID(165), TokenDigest: auth.Digest("session", "expired"), UserID: first.ID, AuthenticationRevision: 1, CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(-time.Minute)}
	if err := repository.CreateSession(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ReadSession(ctx, expired.TokenDigest, now.Add(-24*time.Hour)); !errors.Is(err, auth.ErrSessionRequired) {
		t.Fatalf("expired session accepted with caller-controlled time: %v", err)
	}

	event := auth.AuditEvent{ID: authTestID(166), EventType: "login_succeeded", UserID: first.ID, SessionID: live.ID, CreatedAt: now}
	if err := repository.CreateAuditEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	var eventType, auditUser, auditSession string
	if err := pool.pool.QueryRow(ctx, `SELECT event_type,user_id::text,session_id::text FROM auth_audit_events WHERE id=$1`, event.ID).Scan(&eventType, &auditUser, &auditSession); err != nil || eventType != event.EventType || auditUser != first.ID || auditSession != live.ID {
		t.Fatalf("typed audit event type=%q user=%q session=%q err=%v", eventType, auditUser, auditSession, err)
	}
}

func authTestID(value byte) string {
	return "00000000-0000-4000-8000-0000000000" + string([]byte{"0123456789abcdef"[value>>4], "0123456789abcdef"[value&0x0f]})
}
