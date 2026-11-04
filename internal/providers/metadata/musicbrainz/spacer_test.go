package musicbrainz

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

// MusicBrainz allows about one request per second; calls are spaced, not bursted.
func TestSpacerSeparatesCallsAndHonorsCancellation(t *testing.T) {
	s := &spacer{interval: 60 * time.Millisecond}
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := s.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 110*time.Millisecond {
		t.Fatalf("three calls took %v; they must be at least two intervals apart", elapsed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := s.wait(ctx); err == nil {
		t.Fatal("a cancelled wait must return its error")
	}
}

// fakeClock is time that moves only when a test advances it.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Time
	fn      func()
	stopped bool
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *fakeClock) AfterFunc(d time.Duration, fn func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), fn: fn}
	c.timers = append(c.timers, t)
	return func() bool { c.mu.Lock(); defer c.mu.Unlock(); was := !t.stopped; t.stopped = true; return was }
}

// Advance moves time forward and runs every timer that falls due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	rest := c.timers[:0]
	for _, t := range c.timers {
		switch {
		case t.stopped:
		case !t.at.After(c.now):
			due = append(due, t)
		default:
			rest = append(rest, t)
		}
	}
	c.timers = rest
	c.mu.Unlock()
	for _, t := range due {
		t.fn()
	}
}

func (s *spacer) waiting() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.resolves) + len(s.searches)
}

func waitForQueue(t *testing.T, s *spacer, n int) {
	t.Helper()
	for i := 0; i < 2000 && s.waiting() != n; i++ {
		time.Sleep(time.Millisecond)
	}
	if got := s.waiting(); got != n {
		t.Fatalf("queued = %d, want %d", got, n)
	}
}

// grants records the order in which named callers get their slot.
type grants struct {
	mu    sync.Mutex
	order []string
}

func (g *grants) add(name string) { g.mu.Lock(); g.order = append(g.order, name); g.mu.Unlock() }
func (g *grants) get() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.order...)
}

func TestResolveIsServedBeforeQueuedSearches(t *testing.T) {
	clock := newFakeClock()
	s := &spacer{interval: time.Second, clock: clock}
	if err := s.wait(context.Background()); err != nil { // takes the slot at t0
		t.Fatal(err)
	}
	var got grants
	call := func(name string, search bool) {
		go func() {
			var err error
			if search {
				err = s.waitSearch(context.Background())
			} else {
				err = s.wait(context.Background())
			}
			if err == nil {
				got.add(name)
			}
		}()
	}
	call("search1", true)
	waitForQueue(t, s, 1)
	call("search2", true)
	waitForQueue(t, s, 2)
	call("resolve", false) // arrives last
	waitForQueue(t, s, 3)

	for i, want := range [][]string{{"resolve"}, {"resolve", "search1"}, {"resolve", "search1", "search2"}} {
		clock.Advance(999 * time.Millisecond)
		time.Sleep(5 * time.Millisecond)
		if len(got.get()) != i {
			t.Fatalf("a call was granted before its interval passed: %v", got.get())
		}
		clock.Advance(time.Millisecond)
		for j := 0; j < 2000 && len(got.get()) != len(want); j++ {
			time.Sleep(time.Millisecond)
		}
		if g := got.get(); !slices.Equal(g, want) {
			t.Fatalf("after %d intervals granted %v, want %v", i+1, g, want)
		}
	}
}

func TestSearchQueueIsShortAndAnswersBusy(t *testing.T) {
	clock := newFakeClock()
	s := &spacer{interval: time.Second, clock: clock}
	_ = s.wait(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < maxSearchQueue; i++ {
		go s.waitSearch(ctx)
	}
	waitForQueue(t, s, maxSearchQueue)
	if err := s.waitSearch(context.Background()); !errors.Is(err, metadata.ErrBusy) {
		t.Fatalf("a full queue must answer ErrBusy at once, got %v", err)
	}
	// A resolve is never refused because searches are queued.
	done := make(chan error, 1)
	go func() { done <- s.wait(context.Background()) }()
	waitForQueue(t, s, maxSearchQueue+1)
	clock.Advance(time.Second)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCancelledSearchGivesUpItsSlot(t *testing.T) {
	clock := newFakeClock()
	s := &spacer{interval: time.Second, clock: clock}
	_ = s.wait(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { first <- s.waitSearch(ctx) }()
	waitForQueue(t, s, 1)
	second := make(chan error, 1)
	go func() { second <- s.waitSearch(context.Background()) }()
	waitForQueue(t, s, 2)

	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search returned %v", err)
	}
	waitForQueue(t, s, 1)
	// The cancelled search never used a slot, so the next one is not delayed a further interval.
	clock.Advance(time.Second)
	select {
	case err := <-second:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the surviving search was not served at the first interval")
	}
}
