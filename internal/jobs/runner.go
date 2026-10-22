// Package jobs records background job runs and keeps two runs of the same
// Source from overlapping, including across a process restart (D018). It is
// deliberately small: no queue library, no worker process; a failed run is
// simply retried on the next scheduled tick.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"
)

type Status string

const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// maxErrorLen bounds the stored outcome error; messages must already be safe
// to show an administrator (no URLs, keys or provider response bodies).
const maxErrorLen = 256

var (
	// ErrAlreadyRunning means another run of the same Source holds a live lease.
	ErrAlreadyRunning = errors.New("a run for this source is already in progress")
	// ErrRunLost means the run's lease expired and was reclaimed by another run.
	ErrRunLost = errors.New("run lease was lost")
)

type Run struct {
	ID         string     `json:"id"`
	SourceID   string     `json:"sourceId"`
	Trigger    string     `json:"trigger"`
	Status     Status     `json:"status"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	Error      string     `json:"error,omitempty"`
}

// SourceRuns is one Source's recent runs, newest first. Name is empty until the
// Source has been seen by a sync.
type SourceRuns struct {
	SourceID string `json:"sourceId"`
	Name     string `json:"name"`
	Runs     []Run  `json:"runs"`
}

// Store persists runs. StartRun atomically reclaims runs whose lease expired
// (marking them failed) and fails with ErrAlreadyRunning while a live one exists.
type Store interface {
	StartRun(ctx context.Context, sourceID, trigger string, lease time.Duration, now time.Time) (Run, error)
	Heartbeat(ctx context.Context, runID string, lease time.Duration, now time.Time) error
	FinishRun(ctx context.Context, runID string, status Status, message string, now time.Time) error
	RecentRuns(ctx context.Context, sourceID string, limit int) ([]Run, error)
}

type Runner struct {
	Store Store
	// Lease is how long a run may go without a heartbeat before it is presumed
	// abandoned (its process crashed); Heartbeat must be comfortably shorter.
	Lease     time.Duration
	Heartbeat time.Duration
	Logger    *slog.Logger
	Now       func() time.Time
}

func NewRunner(store Store) *Runner {
	return &Runner{Store: store, Lease: 2 * time.Minute, Heartbeat: 30 * time.Second, Logger: slog.Default(), Now: func() time.Time { return time.Now().UTC() }}
}

// Run executes fn as a recorded run of sourceID. It returns ErrAlreadyRunning
// without calling fn when the Source has a live run. The recorded run is
// returned together with fn's error.
func (r *Runner) Run(ctx context.Context, sourceID, trigger string, fn func(context.Context) error) (Run, error) {
	run, err := r.Store.StartRun(ctx, sourceID, trigger, r.Lease, r.Now())
	if err != nil {
		return Run{}, err
	}
	jobCtx, cancelJob := context.WithCancel(ctx)
	defer cancelJob()
	stopBeat := make(chan struct{})
	beatDone := make(chan struct{})
	go func() {
		defer close(beatDone)
		ticker := time.NewTicker(r.Heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-stopBeat:
				return
			case <-ticker.C:
				err := r.Store.Heartbeat(context.WithoutCancel(ctx), run.ID, r.Lease, r.Now())
				if errors.Is(err, ErrRunLost) {
					cancelJob() // another run owns the Source now; stop working
					return
				}
				if err != nil {
					r.Logger.Warn("job heartbeat failed", "source_id", sourceID, "run_id", run.ID)
				}
			}
		}
	}()
	runErr := safeCall(jobCtx, fn)
	close(stopBeat)
	<-beatDone

	status, message := StatusSucceeded, ""
	if runErr != nil {
		status, message = StatusFailed, truncate(runErr.Error())
		if ctx.Err() != nil {
			message = "interrupted by shutdown"
		}
	}
	now := r.Now()
	// Record the outcome even when shutdown cancelled ctx.
	if err := r.Store.FinishRun(context.WithoutCancel(ctx), run.ID, status, message, now); err != nil {
		return run, errors.Join(runErr, fmt.Errorf("record run outcome: %w", err))
	}
	run.Status, run.Error, run.FinishedAt = status, message, &now
	return run, runErr
}

// Schedule runs fn immediately and then every interval until ctx ends. A failed
// run is retried on the next tick; a start that finds the Source busy retries
// after the lease length instead.
func (r *Runner) Schedule(ctx context.Context, every time.Duration, sourceID string, fn func(context.Context) error) {
	for {
		wait := every
		run, err := r.Run(ctx, sourceID, TriggerSchedule, fn)
		switch {
		case errors.Is(err, ErrAlreadyRunning):
			// Typically a crashed process's run still inside its lease: look
			// again once the lease can have lapsed rather than a full tick later.
			r.Logger.Info("job skipped: source already running", "source_id", sourceID)
			wait = min(every, r.Lease)
		case err != nil && ctx.Err() == nil:
			r.Logger.Warn("job run failed", "source_id", sourceID, "run_id", run.ID, "error", run.Error)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func safeCall(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("job panicked")
		}
	}()
	return fn(ctx)
}

// truncate cuts to maxErrorLen characters (not bytes, which could split a
// UTF-8 sequence), matching the column's character-length check.
func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxErrorLen {
		return s
	}
	return string([]rune(s)[:maxErrorLen])
}
