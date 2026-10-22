//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/jobs"
)

func openJobStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// jobSource returns a unique Source ID and removes its runs afterwards.
func jobSource(t *testing.T, ctx context.Context, s *Store) string {
	t.Helper()
	id := newUUID()
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM job_runs WHERE source_id=$1`, id) })
	return id
}

func TestRunsOfTheSameSourceNeverOverlap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	source, other := jobSource(t, ctx, s), jobSource(t, ctx, s)
	now := time.Now().UTC()
	first, err := s.StartRun(ctx, source, jobs.TriggerSchedule, time.Minute, now)
	if err != nil || first.Status != jobs.StatusRunning {
		t.Fatalf("first start run=%+v err=%v", first, err)
	}
	if _, err := s.StartRun(ctx, source, jobs.TriggerManual, time.Minute, now.Add(time.Second)); !errors.Is(err, jobs.ErrAlreadyRunning) {
		t.Fatalf("overlapping start err=%v", err)
	}
	if _, err := s.StartRun(ctx, other, jobs.TriggerSchedule, time.Minute, now); err != nil {
		t.Fatalf("a different source was blocked: %v", err)
	}
	if err := s.FinishRun(ctx, first.ID, jobs.StatusSucceeded, "", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRun(ctx, source, jobs.TriggerManual, time.Minute, now.Add(3*time.Second)); err != nil {
		t.Fatalf("start after finish: %v", err)
	}
}

func TestGuardSurvivesRestartAndCrashedRunIsReclaimedAfterLeaseExpiry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	before := openJobStore(t, ctx)
	source := jobSource(t, ctx, before)
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	crashed, err := before.StartRun(ctx, source, jobs.TriggerSchedule, time.Minute, t0)
	if err != nil {
		t.Fatal(err)
	}
	// "Restart": the process dies and a fresh pool opens the same database. The
	// guard lives in PostgreSQL, so the lease still blocks a new run.
	before.Close()
	after := openJobStore(t, ctx)
	if _, err := after.StartRun(ctx, source, jobs.TriggerSchedule, time.Minute, t0.Add(30*time.Second)); !errors.Is(err, jobs.ErrAlreadyRunning) {
		t.Fatalf("live lease after restart err=%v", err)
	}
	// Once the lease lapses the abandoned run is failed and a new one starts.
	next, err := after.StartRun(ctx, source, jobs.TriggerSchedule, time.Minute, t0.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("start after lease expiry: %v", err)
	}
	var status, message string
	if err := after.pool.QueryRow(ctx, `SELECT status,COALESCE(error,'') FROM job_runs WHERE id=$1`, crashed.ID).Scan(&status, &message); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || message != "abandoned: lease expired" {
		t.Fatalf("crashed run status=%q error=%q", status, message)
	}
	// The zombie process waking up cannot overwrite the reclaimed run or extend its lease.
	if err := after.FinishRun(ctx, crashed.ID, jobs.StatusSucceeded, "", t0.Add(3*time.Minute)); !errors.Is(err, jobs.ErrRunLost) {
		t.Fatalf("finish of reclaimed run err=%v", err)
	}
	if err := after.Heartbeat(ctx, crashed.ID, time.Minute, t0.Add(3*time.Minute)); !errors.Is(err, jobs.ErrRunLost) {
		t.Fatalf("heartbeat of reclaimed run err=%v", err)
	}
	if err := after.Heartbeat(ctx, next.ID, time.Minute, t0.Add(2*time.Minute+30*time.Second)); err != nil {
		t.Fatalf("heartbeat of live run: %v", err)
	}
	// The heartbeat extended the live run's lease past its original expiry.
	if _, err := after.StartRun(ctx, source, jobs.TriggerSchedule, time.Minute, t0.Add(3*time.Minute+15*time.Second)); !errors.Is(err, jobs.ErrAlreadyRunning) {
		t.Fatalf("heartbeat did not extend lease: %v", err)
	}
}

func TestFailedRunIsRecordedAndNextRunStarts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	source := jobSource(t, ctx, s)
	runner := jobs.NewRunner(s)
	failed, err := runner.Run(ctx, source, jobs.TriggerSchedule, func(context.Context) error { return errors.New("inventory request failed") })
	if err == nil || failed.Status != jobs.StatusFailed {
		t.Fatalf("failed run=%+v err=%v", failed, err)
	}
	ok, err := runner.Run(ctx, source, jobs.TriggerSchedule, func(context.Context) error { return nil })
	if err != nil || ok.Status != jobs.StatusSucceeded {
		t.Fatalf("retry run=%+v err=%v", ok, err)
	}
	runs, err := s.RecentRuns(ctx, source, 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	if runs[0].ID != ok.ID || runs[0].Status != jobs.StatusSucceeded || runs[0].FinishedAt == nil || runs[0].Error != "" {
		t.Fatalf("newest run=%+v", runs[0])
	}
	if runs[1].ID != failed.ID || runs[1].Status != jobs.StatusFailed || runs[1].Error != "inventory request failed" || runs[1].FinishedAt == nil || runs[1].StartedAt.IsZero() {
		t.Fatalf("older run=%+v", runs[1])
	}
	limited, err := s.RecentRuns(ctx, source, 1)
	if err != nil || len(limited) != 1 || limited[0].ID != ok.ID {
		t.Fatalf("limit runs=%+v err=%v", limited, err)
	}
}

func TestRecentRunsBySourceGroupsAndLimitsPerSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	a, b := jobSource(t, ctx, s), jobSource(t, ctx, s)
	t0 := time.Now().UTC().Truncate(time.Millisecond)
	for i, source := range []string{a, a, a, b} {
		run, err := s.StartRun(ctx, source, jobs.TriggerSchedule, time.Minute, t0.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.FinishRun(ctx, run.ID, jobs.StatusSucceeded, "", t0.Add(time.Duration(i)*time.Second+time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	groups, err := s.RecentRunsBySource(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, g := range groups {
		counts[g.SourceID] = len(g.Runs)
		for i := 1; i < len(g.Runs); i++ {
			if g.Runs[i].StartedAt.After(g.Runs[i-1].StartedAt) {
				t.Fatalf("runs for %s not newest first", g.SourceID)
			}
		}
	}
	if counts[a] != 2 || counts[b] != 1 {
		t.Fatalf("counts a=%d b=%d, want 2 and 1", counts[a], counts[b])
	}
}
