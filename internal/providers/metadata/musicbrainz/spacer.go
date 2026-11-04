package musicbrainz

import (
	"context"
	"sync"
	"time"

	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

// maxSearchQueue is how many searches may wait for a slot. A search that
// arrives beyond it is refused at once as busy: a long queue of name searches
// would only make every one of them answer too late to be useful.
const maxSearchQueue = 3

// clock is time, replaceable in tests.
type clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

type waiter struct{ granted chan struct{} }

// spacer keeps calls at least interval apart. MusicBrainz allows about one
// request per second per client, and answers 503 above that.
//
// Callers wait in two lines. Resolves (a person previewing a result, tracklists,
// alias lookups) are always served before searches, so clicking a result never
// waits behind searches. The search line is short and a search that gives up
// leaves it without having used a slot.
type spacer struct {
	interval time.Duration
	clock    clock // nil means real time

	mu       sync.Mutex
	next     time.Time
	armed    bool
	resolves []*waiter
	searches []*waiter
}

// apiSpacer is shared by every Resolver and Searcher in the process, so
// concurrent previews and searches queue behind one another instead of bursting.
var apiSpacer = &spacer{interval: DefaultMinInterval}

// wait blocks until a resolve-priority call may be made, or the context ends.
func (s *spacer) wait(ctx context.Context) error { return s.acquire(ctx, false) }

// waitSearch is wait for a name search: lower priority, a bounded queue, and
// metadata.ErrBusy when that queue is full.
func (s *spacer) waitSearch(ctx context.Context) error { return s.acquire(ctx, true) }

func (s *spacer) acquire(ctx context.Context, search bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w := &waiter{granted: make(chan struct{})}
	s.mu.Lock()
	if search {
		if len(s.searches) >= maxSearchQueue {
			s.mu.Unlock()
			return metadata.ErrBusy
		}
		s.searches = append(s.searches, w)
	} else {
		s.resolves = append(s.resolves, w)
	}
	s.dispatchLocked()
	s.mu.Unlock()

	select {
	case <-w.granted:
		return nil
	case <-ctx.Done():
		s.mu.Lock()
		defer s.mu.Unlock()
		s.resolves, s.searches = remove(s.resolves, w), remove(s.searches, w)
		return ctx.Err()
	}
}

func remove(list []*waiter, w *waiter) []*waiter {
	for i, x := range list {
		if x == w {
			return append(list[:i:i], list[i+1:]...)
		}
	}
	return list
}

// dispatchLocked grants the next slot if it is due, or arranges to when it is.
// The waiter is chosen when the slot opens, not when the timer is set, so a
// resolve that arrives meanwhile goes first.
func (s *spacer) dispatchLocked() {
	c := s.clock
	if c == nil {
		c = realClock{}
	}
	for !s.armed && len(s.resolves)+len(s.searches) > 0 {
		now := c.Now()
		if now.Before(s.next) {
			s.armed = true
			c.AfterFunc(s.next.Sub(now), func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				s.armed = false
				s.dispatchLocked()
			})
			return
		}
		var w *waiter
		if len(s.resolves) > 0 {
			w, s.resolves = s.resolves[0], s.resolves[1:]
		} else {
			w, s.searches = s.searches[0], s.searches[1:]
		}
		s.next = now.Add(s.interval)
		close(w.granted)
	}
}
