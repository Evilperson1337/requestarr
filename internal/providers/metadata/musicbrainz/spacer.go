package musicbrainz

import (
	"context"
	"sync"
	"time"
)

// spacer keeps calls at least interval apart. MusicBrainz allows about one
// request per second per client, and answers 503 above that.
type spacer struct {
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
}

// apiSpacer is shared by every Resolver in the process, so concurrent previews
// queue behind one another instead of bursting.
var apiSpacer = &spacer{interval: DefaultMinInterval}

// wait blocks until a call may be made, or the context ends.
func (s *spacer) wait(ctx context.Context) error {
	s.mu.Lock()
	now := time.Now()
	at := s.next
	if at.Before(now) {
		at = now
	}
	s.next = at.Add(s.interval)
	s.mu.Unlock()
	if delay := time.Until(at); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
