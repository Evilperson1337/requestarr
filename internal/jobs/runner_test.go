package jobs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// memStore is a deterministic in-memory Store: one running run per source.
type memStore struct {
	mu           sync.Mutex
	runs         []*Run
	heartbeats   int
	lostRun      bool
	heartbeatErr error
}

func (m *memStore) StartRun(_ context.Context, sourceID, trigger string, _ time.Duration, now time.Time) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.SourceID == sourceID && r.Status == StatusRunning {
			return Run{}, ErrAlreadyRunning
		}
	}
	run := &Run{ID: string(rune('a' + len(m.runs))), SourceID: sourceID, Trigger: trigger, Status: StatusRunning, StartedAt: now}
	m.runs = append(m.runs, run)
	return *run, nil
}

func (m *memStore) Heartbeat(context.Context, string, time.Duration, time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeats++
	if m.lostRun {
		return ErrRunLost
	}
	return m.heartbeatErr
}

func (m *memStore) FinishRun(_ context.Context, id string, status Status, message string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.ID == id {
			r.Status, r.Error, r.FinishedAt = status, message, &now
		}
	}
	return nil
}

func (m *memStore) RecentRuns(context.Context, string, int) ([]Run, error) { return nil, nil }

func (m *memStore) snapshot() []Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Run, len(m.runs))
	for i, r := range m.runs {
		out[i] = *r
	}
	return out
}

func TestRunRecordsOutcomeAndError(t *testing.T) {
	store := &memStore{}
	runner := NewRunner(store)
	if run, err := runner.Run(context.Background(), "src", TriggerManual, func(context.Context) error { return nil }); err != nil || run.Status != StatusSucceeded {
		t.Fatalf("success run=%+v err=%v", run, err)
	}
	run, err := runner.Run(context.Background(), "src", TriggerSchedule, func(context.Context) error { return errors.New("inventory request failed") })
	if err == nil || run.Status != StatusFailed || run.Error != "inventory request failed" {
		t.Fatalf("failed run=%+v err=%v", run, err)
	}
	got := store.snapshot()
	if len(got) != 2 || got[0].Status != StatusSucceeded || got[0].FinishedAt == nil || got[1].Status != StatusFailed || got[1].Error != "inventory request failed" || got[1].Trigger != TriggerSchedule {
		t.Fatalf("recorded runs=%+v", got)
	}
}

func TestRunSkipsWorkWhenSourceAlreadyRunning(t *testing.T) {
	store := &memStore{}
	runner := NewRunner(store)
	release := make(chan struct{})
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runner.Run(context.Background(), "src", TriggerSchedule, func(context.Context) error { close(started); <-release; return nil })
	}()
	<-started
	ran := false
	if _, err := runner.Run(context.Background(), "src", TriggerManual, func(context.Context) error { ran = true; return nil }); !errors.Is(err, ErrAlreadyRunning) || ran {
		t.Fatalf("overlapping run err=%v ran=%v", err, ran)
	}
	if _, err := runner.Run(context.Background(), "other", TriggerManual, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("other source blocked: %v", err)
	}
	close(release)
	<-done
}

func TestRunRecordsPanicAsFailureAndTruncatesError(t *testing.T) {
	store := &memStore{}
	runner := NewRunner(store)
	run, err := runner.Run(context.Background(), "src", TriggerSchedule, func(context.Context) error { panic("boom") })
	if err == nil || run.Status != StatusFailed || run.Error != "job panicked" {
		t.Fatalf("panic run=%+v err=%v", run, err)
	}
	long := make([]byte, 1000)
	for i := range long {
		long[i] = 'x'
	}
	run, _ = runner.Run(context.Background(), "src", TriggerSchedule, func(context.Context) error { return errors.New(string(long)) })
	if len(run.Error) > maxErrorLen {
		t.Fatalf("error length %d exceeds %d", len(run.Error), maxErrorLen)
	}
}

func TestRunHeartbeatsWhileWorking(t *testing.T) {
	store := &memStore{}
	runner := NewRunner(store)
	runner.Heartbeat = time.Millisecond
	_, err := runner.Run(context.Background(), "src", TriggerSchedule, func(context.Context) error {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			store.mu.Lock()
			n := store.heartbeats
			store.mu.Unlock()
			if n >= 2 {
				return nil
			}
			time.Sleep(time.Millisecond)
		}
		return errors.New("no heartbeats")
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestScheduleRetriesFailedRunsOnNextTick(t *testing.T) {
	store := &memStore{}
	runner := NewRunner(store)
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		runner.Schedule(ctx, 5*time.Millisecond, "src", func(context.Context) error {
			calls++
			if calls == 1 {
				return errors.New("inventory request failed")
			}
			if calls == 3 {
				cancel()
			}
			return nil
		})
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop")
	}
	got := store.snapshot()
	if len(got) < 3 || got[0].Status != StatusFailed || got[1].Status != StatusSucceeded || got[0].Trigger != TriggerSchedule {
		t.Fatalf("runs=%+v", got)
	}
}

func TestScheduleRetriesSoonWhenSourceWasBusyAtStart(t *testing.T) {
	store := &memStore{}
	runner := NewRunner(store)
	runner.Lease = 10 * time.Millisecond
	// A run left over from a crashed process holds the Source until it ends.
	stale, err := store.StartRun(context.Background(), "src", TriggerSchedule, runner.Lease, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(30*time.Millisecond, func() {
		_ = store.FinishRun(context.Background(), stale.ID, StatusFailed, "abandoned: lease expired", time.Now())
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ran := make(chan struct{}, 1)
	go runner.Schedule(ctx, time.Hour, "src", func(context.Context) error {
		select {
		case ran <- struct{}{}:
		default:
		}
		return nil
	})
	select {
	case <-ran:
	case <-ctx.Done():
		t.Fatal("a busy start waited a full tick instead of retrying after the lease")
	}
}

func TestShutdownIsRecordedAsInterruptedNotAsAFailure(t *testing.T) {
	store := &memStore{}
	runner := NewRunner(store)
	ctx, cancel := context.WithCancel(context.Background())
	run, err := runner.Run(ctx, "src", TriggerSchedule, func(ctx context.Context) error {
		cancel()
		return ctx.Err()
	})
	if err == nil || run.Status != StatusFailed || run.Error != "interrupted by shutdown" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
}

func TestHeartbeatFailuresAreLogged(t *testing.T) {
	store := &memStore{heartbeatErr: errors.New("db blip")}
	var logs bytes.Buffer
	runner := NewRunner(store)
	runner.Heartbeat = time.Millisecond
	runner.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	_, _ = runner.Run(context.Background(), "src", TriggerSchedule, func(context.Context) error {
		time.Sleep(30 * time.Millisecond)
		return nil
	})
	if !strings.Contains(logs.String(), "heartbeat") {
		t.Fatalf("heartbeat failure was not logged: %q", logs.String())
	}
}

func TestTruncatedErrorStaysValidUTF8WithinTheCharacterLimit(t *testing.T) {
	runner := NewRunner(&memStore{})
	run, _ := runner.Run(context.Background(), "src", TriggerSchedule, func(context.Context) error {
		return errors.New(strings.Repeat("é", 400))
	})
	if !utf8.ValidString(run.Error) || utf8.RuneCountInString(run.Error) != maxErrorLen {
		t.Fatalf("error has %d runes, valid=%v; want %d valid runes", utf8.RuneCountInString(run.Error), utf8.ValidString(run.Error), maxErrorLen)
	}
}
