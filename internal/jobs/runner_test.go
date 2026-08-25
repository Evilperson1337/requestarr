package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type onceExecutorFake struct {
	started chan struct{}
	block   <-chan struct{}
	err     error
	mu      sync.Mutex
	calls   int
}

func (f *onceExecutorFake) RunOnce(context.Context) (bool, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.block != nil {
		<-f.block
	}
	return false, f.err
}

type producerFake struct {
	name     string
	interval time.Duration
	called   chan struct{}
}

func (f producerFake) Name() string                  { return f.name }
func (f producerFake) Interval() time.Duration       { return f.interval }
func (f producerFake) Produce(context.Context) error { f.called <- struct{}{}; return nil }

func TestRunnerStartsBoundedWorkersAndStops(t *testing.T) {
	executor := &onceExecutorFake{started: make(chan struct{}, 3)}
	runner, err := NewRunner(executor, Options{Workers: 2, PollInterval: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-executor.started:
		case <-time.After(time.Second):
			t.Fatal("worker did not start")
		}
	}
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	executor.mu.Lock()
	calls := executor.calls
	executor.mu.Unlock()
	if calls != 2 {
		t.Fatalf("worker calls = %d, want 2", calls)
	}
}

func TestRunnerRunsLocalProducerAndSafelyObservesErrors(t *testing.T) {
	called := make(chan struct{}, 1)
	runner, err := NewRunner(&onceExecutorFake{err: errors.New("unavailable")}, Options{Workers: 1, PollInterval: time.Minute, Producers: []PeriodicProducer{producerFake{name: "local", interval: time.Minute, called: called}}, Observer: panicObserver{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("producer did not run")
	}
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type panicObserver struct{}

func (panicObserver) Observe(context.Context, Event) { panic("observer failure") }

func TestNewRunnerRejectsUnboundedOptions(t *testing.T) {
	_, err := NewRunner(&onceExecutorFake{}, Options{Workers: maxRunnerWorkers + 1, PollInterval: time.Second})
	if !errors.Is(err, ErrInvalidRunner) {
		t.Fatalf("NewRunner() error = %v", err)
	}
}
