//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"
)

// A claim released because the sender was switched off must not cost an
// attempt: the event is pending again with its earlier attempt count.
func TestReleasedGlobalAppriseClaimRestoresTheAttemptCount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	key := "test-release-" + newUUID()
	// Oldest available_at, so this event is claimed before any other pending one.
	if _, err := s.pool.Exec(ctx, `INSERT INTO notification_outbox(event_key,title,body,attempt_count,available_at,created_at) VALUES($1,'release test','body',2,'2000-01-01','2000-01-01')`, key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM notification_outbox WHERE event_key=$1`, key)
	})

	state := func() (status string, attempts int, leased bool) {
		t.Helper()
		if err := s.pool.QueryRow(ctx, `SELECT status,attempt_count,lease_token IS NOT NULL FROM notification_outbox WHERE event_key=$1`, key).Scan(&status, &attempts, &leased); err != nil {
			t.Fatal(err)
		}
		return
	}
	id, _, _, token, attempts, err := s.ClaimGlobalApprise(ctx, 30*time.Second)
	if err != nil || id == "" || attempts != 3 {
		t.Fatalf("claim = %q attempts=%d err=%v, want our event at attempt 3", id, attempts, err)
	}
	if status, n, leased := state(); status != "pending" || n != 3 || !leased {
		t.Fatalf("claimed state = %s/%d/leased=%v", status, n, leased)
	}
	// A stale or wrong lease releases nothing.
	if err := s.ReleaseGlobalApprise(ctx, id, newUUID()); err != nil {
		t.Fatal(err)
	}
	if _, n, leased := state(); n != 3 || !leased {
		t.Fatalf("a wrong lease changed the event: attempts=%d leased=%v", n, leased)
	}
	if err := s.ReleaseGlobalApprise(ctx, id, token); err != nil {
		t.Fatal(err)
	}
	if status, n, leased := state(); status != "pending" || n != 2 || leased {
		t.Fatalf("released state = %s/%d/leased=%v, want pending/2/not leased", status, n, leased)
	}
	// It is claimable again immediately and counts the next attempt as the third.
	again, _, _, _, attempts, err := s.ClaimGlobalApprise(ctx, 30*time.Second)
	if err != nil || again != id || attempts != 3 {
		t.Fatalf("reclaim = %q attempts=%d err=%v, want the same event at attempt 3", again, attempts, err)
	}
}
