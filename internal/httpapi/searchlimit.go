package httpapi

import (
	"math"
	"sync"
	"time"
)

// Default search budget per person: a burst of 10 searches, then one more every
// second (60 a minute). Typeahead at a 300 ms pause stays well inside this for ordinary
// typing; a script or stuck client does not.
const (
	searchLimitBurst  = 10
	searchLimitRefill = time.Second
	// Above this many tracked people, full buckets are forgotten.
	searchLimitSweepAt = 1024
)

// SearchLimiter is a small in-process token bucket per user, protecting the
// provider budgets the whole household shares from one client. It is not
// persisted and not shared between processes.
type SearchLimiter struct {
	burst  float64
	refill time.Duration
	now    func() time.Time

	mu      sync.Mutex
	buckets map[string]searchBucket
}

type searchBucket struct {
	tokens float64
	at     time.Time
}

// NewSearchLimiter allows burst searches at once and one more per refill.
// now is injectable for tests.
func NewSearchLimiter(burst int, refill time.Duration, now func() time.Time) *SearchLimiter {
	if now == nil {
		now = time.Now
	}
	return &SearchLimiter{burst: float64(burst), refill: refill, now: now, buckets: map[string]searchBucket{}}
}

// Allow spends one token for the user. When none is left it reports how long
// until one is.
func (l *SearchLimiter) Allow(userID string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, found := l.buckets[userID]
	if !found {
		b = searchBucket{tokens: l.burst, at: now}
	}
	if elapsed := now.Sub(b.at); elapsed > 0 {
		b.tokens = math.Min(l.burst, b.tokens+float64(elapsed)/float64(l.refill))
	}
	b.at = now
	if b.tokens < 1 {
		l.buckets[userID] = b
		return false, time.Duration((1 - b.tokens) * float64(l.refill))
	}
	b.tokens--
	l.buckets[userID] = b
	if len(l.buckets) > searchLimitSweepAt {
		l.sweep(now)
	}
	return true, 0
}

// sweep forgets people whose bucket has refilled completely. Caller holds mu.
func (l *SearchLimiter) sweep(now time.Time) {
	for id, b := range l.buckets {
		if b.tokens+float64(now.Sub(b.at))/float64(l.refill) >= l.burst {
			delete(l.buckets, id)
		}
	}
}
