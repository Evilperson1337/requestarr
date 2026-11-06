package auth

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func (f *fakeLocalStore) rows(typ string) (rows, attempts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.Type == typ {
			rows++
			attempts += max(e.Count, 1)
		}
	}
	return
}

// S2: the audit cannot grow without bound under a flood, loses no count, and
// never drops a success or a password change.

func TestAFloodOfFailedSignInsWritesABoundedNumberOfRowsThatStillCountEveryAttempt(t *testing.T) {
	f := newFixture(t)
	const flood = 10000
	for i := 0; i < flood; i++ {
		f.login("made-up-"+strconv.Itoa(i), "x", fromAddr("198.51.100."+strconv.Itoa(i%200+1)))
	}
	f.h.FlushAudit(context.Background())
	rows, attempts := f.store.rows(AuditSignInFailure)
	if rows > maxAuditRowsPerMinute {
		t.Fatalf("failure rows=%d after a flood of %d, want at most %d", rows, flood, maxAuditRowsPerMinute)
	}
	if attempts != flood {
		t.Fatalf("rows stand for %d attempts, want %d", attempts, flood)
	}
}

func TestOneRowPerAccountAndPerAddressPerMinute(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 500; i++ { // one account, many addresses
		f.login("admin", "wrong", fromAddr("198.51.100."+strconv.Itoa(i%200+1)))
	}
	if rows, _ := f.store.rows(AuditSignInFailure); rows != 1 {
		t.Fatalf("one account in one minute wrote %d rows", rows)
	}
	for i := 0; i < 500; i++ { // one address, many names
		f.login("n"+strconv.Itoa(i), "wrong", fromAddr("192.0.2.9"))
	}
	if rows, _ := f.store.rows(AuditSignInFailure); rows != 2 {
		t.Fatalf("one address in one minute added %d rows", rows-1)
	}
	f.clock.Advance(61 * time.Second)
	f.login("admin", "wrong", fromAddr("198.51.100.1"))
	f.h.FlushAudit(context.Background())
	rows, attempts := f.store.rows(AuditSignInFailure)
	if rows != 3 || attempts != 1001 {
		t.Fatalf("rows=%d attempts=%d, want a new row for the next minute and 1001 attempts", rows, attempts)
	}
}

func TestThrottledAttemptsAreCoalescedToo(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 10000; i++ {
		_ = f.h.auditAttempt(context.Background(), AuditSignInThrottled, "", "", "name-"+strconv.Itoa(i), "198.51.100."+strconv.Itoa(i%250+1))
	}
	f.h.FlushAudit(context.Background())
	rows, attempts := f.store.rows(AuditSignInThrottled)
	if rows > maxAuditRowsPerMinute || attempts != 10000 {
		t.Fatalf("rows=%d attempts=%d", rows, attempts)
	}
}

func TestSuccessesAndPasswordChangesAreNeverDropped(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 2000; i++ {
		f.login("made-up-"+strconv.Itoa(i), "x", fromAddr("198.51.100.5"))
	}
	signIns := 0
	for i := 0; i < 25; i++ {
		if w := f.login("admin", testPassword, fromAddr("192.0.2.1")); w.Code != http.StatusOK {
			t.Fatalf("status=%d", w.Code)
		}
		signIns++
		f.login("made-up-again", "x", fromAddr("198.51.100.5"))
	}
	cs := f.signIn()
	signIns++
	if w := f.authed(http.MethodPost, "/auth/local/password", changeBody(testPassword, goodNewPassword), cs); w.Code != http.StatusNoContent {
		t.Fatalf("change status=%d", w.Code)
	}
	if rows, _ := f.store.rows(AuditSignInSuccess); rows != signIns {
		t.Fatalf("success rows=%d, want %d", rows, signIns)
	}
	if rows, _ := f.store.rows(AuditPasswordChanged); rows != 1 {
		t.Fatalf("password change rows=%d", rows)
	}
}

func TestAnAuditFailureOnTheFirstRowIsReportedAndDoesNotOpenAWindow(t *testing.T) {
	f := newFixture(t)
	f.store.failAudit = true
	if err := f.h.auditAttempt(context.Background(), AuditSignInFailure, "", "", "x", "198.51.100.1"); err == nil {
		t.Fatal("audit error swallowed")
	}
	f.store.failAudit = false
	if err := f.h.auditAttempt(context.Background(), AuditSignInFailure, "", "", "x", "198.51.100.1"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := f.store.rows(AuditSignInFailure); rows != 1 {
		t.Fatalf("rows=%d", rows)
	}
}

func TestMaintenancePrunesThroughTheStoreAtStartAndFlushesCounts(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 5; i++ {
		f.login("admin", "wrong", fromAddr("198.51.100.1"))
	}
	f.clock.Advance(2 * time.Minute)
	f.h.maintainAudit(context.Background())
	if _, attempts := f.store.rows(AuditSignInFailure); attempts != 5 {
		t.Fatalf("attempts=%d after the maintenance flush", attempts)
	}
	if got := f.store.prunedBefore; !got.Equal(f.clock.Now().Add(-auditRetention)) {
		t.Fatalf("pruned before %v, want now minus %v", got, auditRetention)
	}
	if auditRetention != 90*24*time.Hour {
		t.Fatalf("retention=%v", auditRetention)
	}
}
