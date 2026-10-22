package notifications

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeOutbox is written by the worker goroutine while tests poll it, so state
// is guarded by mu; tests poll through isCompleted.
type fakeOutbox struct {
	mu                 sync.Mutex
	claimed, completed bool
	success            bool
	claimErr           error
	completeErr        error
	completeID         string
	completeToken      string
	completeDelay      time.Duration
	claims             int
	claimAttempts      int
}

func (f *fakeOutbox) isCompleted() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.completed
}

func (f *fakeOutbox) ClaimGlobalApprise(context.Context, time.Duration) (string, string, string, string, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	if f.claimErr != nil {
		return "", "", "", "", 0, f.claimErr
	}
	if f.claimed {
		return "", "", "", "", 0, nil
	}
	f.claimed = true
	return "delivery", "title", "body", "lease", f.claimAttemptsOrDefault(), nil
}
func (f *fakeOutbox) claimAttemptsOrDefault() int {
	if f.claimAttempts == 0 {
		return 1
	}
	return f.claimAttempts
}
func (f *fakeOutbox) CompleteGlobalApprise(_ context.Context, id, token string, success bool, delay time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != "delivery" || token != "lease" {
		return errors.New("bad lease")
	}
	f.completed = true
	f.success = success
	f.completeID, f.completeToken, f.completeDelay = id, token, delay
	return f.completeErr
}

type fakeSender struct {
	err error
	got Message
}

func (f *fakeSender) Send(_ context.Context, m Message) error { f.got = m; return f.err }

func TestRunWorkerDeliversGlobalEventAndCompletesLease(t *testing.T) {
	outbox := &fakeOutbox{}
	sender := &fakeSender{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunWorker(ctx, outbox, sender) }()
	deadline := time.After(time.Second)
	for !outbox.isCompleted() {
		select {
		case <-deadline:
			t.Fatal("worker did not complete")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
	if !outbox.success || sender.got.Title != "title" || sender.got.Body != "body" {
		t.Fatalf("delivery=%+v success=%v", sender.got, outbox.success)
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	if got := retryDelay(1); got != 2*time.Second {
		t.Fatalf("retryDelay(1)=%s", got)
	}
	if got := retryDelay(100); got != time.Hour {
		t.Fatalf("retryDelay(100)=%s, want 1h", got)
	}
	if got := retryDelay(-1); got != time.Second {
		t.Fatalf("retryDelay(-1)=%s", got)
	}
}

func TestRunWorkerSchedulesRetryAfterSendFailure(t *testing.T) {
	outbox := &fakeOutbox{claimAttempts: 3}
	sender := &fakeSender{err: errors.New("temporary provider outage")}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunWorker(ctx, outbox, sender) }()
	waitFor(t, outbox.isCompleted)
	cancel()
	<-done
	if outbox.success {
		t.Fatal("failed send was recorded as successful")
	}
	if outbox.completeDelay != 8*time.Second {
		t.Fatalf("retry delay = %s, want 8s", outbox.completeDelay)
	}
}

func TestRunWorkerTerminalAttemptStillRecordsFailure(t *testing.T) {
	outbox := &fakeOutbox{claimAttempts: 8}
	sender := &fakeSender{err: errors.New("permanent provider rejection")}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunWorker(ctx, outbox, sender) }()
	waitFor(t, outbox.isCompleted)
	cancel()
	<-done
	if outbox.success {
		t.Fatal("terminal failed send was recorded as successful")
	}
	if outbox.completeDelay != 256*time.Second {
		t.Fatalf("terminal retry delay = %s, want 256s backoff", outbox.completeDelay)
	}
}

func TestRunWorkerStopsOnOutboxFailures(t *testing.T) {
	t.Run("claim", func(t *testing.T) {
		want := errors.New("database unavailable")
		outbox := &fakeOutbox{claimErr: want}
		err := RunWorker(context.Background(), outbox, &fakeSender{})
		if !errors.Is(err, want) {
			t.Fatalf("RunWorker error = %v, want wrapped %v", err, want)
		}
	})
	t.Run("completion", func(t *testing.T) {
		want := errors.New("lease update failed")
		outbox := &fakeOutbox{completeErr: want}
		err := RunWorker(context.Background(), outbox, &fakeSender{})
		if !errors.Is(err, want) {
			t.Fatalf("RunWorker error = %v, want wrapped %v", err, want)
		}
	})
}

func TestRunWorkerReturnsContextCancellationWhenIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunWorker(ctx, &fakeOutbox{claimed: true}, &fakeSender{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunWorker error = %v, want context.Canceled", err)
	}
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !ready() {
		select {
		case <-timer.C:
			t.Fatal("condition was not met before deadline")
		case <-ticker.C:
		}
	}
}
