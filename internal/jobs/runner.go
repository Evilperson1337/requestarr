package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrInvalidRunner = errors.New("invalid job runner")

const (
	maxRunnerWorkers = 32
	maxPollInterval  = time.Minute
)

// OnceExecutor is the narrow durable execution boundary used by Runner.
type OnceExecutor interface {
	RunOnce(context.Context) (bool, error)
}

// PeriodicProducer is an optional process-local producer. It may enqueue
// durable work, but must not run a job handler or make product integrations
// directly. The Runner owns its lifetime and serializes each producer.
type PeriodicProducer interface {
	Name() string
	Interval() time.Duration
	Produce(context.Context) error
}

// Event contains safe runner diagnostics. Errors are deliberately represented
// only by a stable category so observer implementations cannot accidentally log
// job payloads, handler errors, or credentials.
type Event struct {
	Component string
	Name      string
	Category  string
}

// Observer receives best-effort runner events. Observer failures and panics
// never affect durable job processing.
type Observer interface {
	Observe(context.Context, Event)
}

type noopObserver struct{}

func (noopObserver) Observe(context.Context, Event) {}

// Options bounds local concurrency and polling. All scheduling is local; job
// ownership and outcomes remain durable repository responsibilities.
type Options struct {
	Workers      int
	PollInterval time.Duration
	Producers    []PeriodicProducer
	Observer     Observer
}

// Runner drives a bounded number of durable Executor worker loops and optional
// local producers. Start and Stop are safe lifecycle boundaries for application
// startup and shutdown.
type Runner struct {
	executor     OnceExecutor
	workers      int
	pollInterval time.Duration
	producers    []PeriodicProducer
	observer     Observer

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewRunner(executor OnceExecutor, options Options) (*Runner, error) {
	if executor == nil || options.Workers < 1 || options.Workers > maxRunnerWorkers || options.PollInterval <= 0 || options.PollInterval > maxPollInterval {
		return nil, fmt.Errorf("%w: executor, 1-%d workers, and a poll interval up to %s are required", ErrInvalidRunner, maxRunnerWorkers, maxPollInterval)
	}
	for _, producer := range options.Producers {
		if producer == nil || !validText(producer.Name(), maxJobKindLength) || producer.Interval() <= 0 || producer.Interval() > maxPollInterval {
			return nil, fmt.Errorf("%w: producer name and interval up to %s are required", ErrInvalidRunner, maxPollInterval)
		}
	}
	observer := options.Observer
	if observer == nil {
		observer = noopObserver{}
	}
	return &Runner{executor: executor, workers: options.Workers, pollInterval: options.PollInterval, producers: append([]PeriodicProducer(nil), options.Producers...), observer: observer}, nil
}

// Start launches workers and producers. Calling Start while already running is
// rejected; callers must Stop and wait before a subsequent Start.
func (r *Runner) Start(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("%w: runner is nil", ErrInvalidRunner)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return fmt.Errorf("%w: runner is already started", ErrInvalidRunner)
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.done = make(chan struct{})
	var wg sync.WaitGroup
	for range r.workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.worker(runCtx)
		}()
	}
	for _, producer := range r.producers {
		wg.Add(1)
		go func(producer PeriodicProducer) {
			defer wg.Done()
			r.produce(runCtx, producer)
		}(producer)
	}
	go func(done chan struct{}) {
		wg.Wait()
		close(done)
	}(r.done)
	return nil
}

// Stop cancels all local loops and waits for them to join. The supplied context
// bounds shutdown waiting but does not cancel the durable outcome context used
// by a worker before cancellation was requested.
func (r *Runner) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		r.mu.Lock()
		if r.done == done {
			r.cancel, r.done = nil, nil
		}
		r.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) worker(ctx context.Context) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		ran, err := r.executor.RunOnce(ctx)
		if err != nil {
			r.observe(ctx, Event{Component: "worker", Category: "execution_failed"})
		}
		if ran && err == nil {
			continue
		}
		if !wait(ctx, r.pollInterval) {
			return
		}
	}
}

func (r *Runner) produce(ctx context.Context, producer PeriodicProducer) {
	for {
		if err := producer.Produce(ctx); err != nil && ctx.Err() == nil {
			r.observe(ctx, Event{Component: "producer", Name: producer.Name(), Category: "produce_failed"})
		}
		if !wait(ctx, producer.Interval()) {
			return
		}
	}
}

func (r *Runner) observe(ctx context.Context, event Event) {
	defer func() { _ = recover() }()
	r.observer.Observe(ctx, event)
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
