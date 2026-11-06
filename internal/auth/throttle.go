package auth

import (
	"context"
	"sync"
	"time"
)

// Sign-in abuse protection (D040). Nothing here ever refuses or delays a
// correct password because of other people's failures:
//
//   - A FAILED attempt is answered after a delay that grows with the failures
//     recorded for its account name and client address (Fail returns it). A
//     request is never refused for having a delay, so the right password goes
//     straight through.
//   - Password verifications are CPU-bound, so only a few run at once: a global
//     bound, a smaller bound per account and per address, and one slot kept
//     back for a client address that has not failed (Admit). A flood from
//     addresses that already failed can therefore never take the last slot.
//     Requests over a bound wait briefly, then are turned away without any
//     hashing.
//   - Tracked state is bounded. Eviction only drops expired or low-count
//     entries that carry no delay; when nothing can be dropped a new key is not
//     tracked at all, so a flood of made-up names cannot reset anyone's delay.
//
// State is in memory in the one process, so a restart clears it.
const (
	delayBase    = time.Second
	delayCap     = 15 * time.Second // held on the response, so it stays under the server write timeout
	accountGrace = 2                // typos allowed per account before the first delay
	addressGrace = 5                // allowed per client address; looser, because a reverse proxy can put everyone behind one address
	forgetAfter  = time.Hour
	maxTracked   = 10000
	maxFailures  = 64 // keeps the doubling in delayFor bounded

	maxVerifications      = 4   // concurrent password verifications, whoever asks
	reservedVerifications = 1   // of those, kept for addresses with no recent failure
	perKeyVerifications   = 2   // concurrent verifications per account name and per address
	maxWaiters            = 64  // requests queued for a slot; more are turned away at once
	maxHolds              = 256 // failed responses being held; more are answered without a hold
	defaultQueueWait      = 2 * time.Second
	sweepEvery            = time.Minute
	evictionSample        = 64
)

// Throttle tracks failed attempts and bounds concurrent verifications. The zero
// value is not usable; use NewThrottle.
type Throttle struct {
	now func() time.Time
	// QueueWait is how long a request waits for a verification slot.
	QueueWait time.Duration

	mu        sync.Mutex
	accounts  map[string]*attempts
	addrs     map[string]*attempts
	swept     [2]time.Time
	inUse     int
	byAccount map[string]int
	byAddr    map[string]int
	waiters   int
	holds     int
	changed   chan struct{} // closed and replaced whenever a slot frees
}

type attempts struct {
	failures int
	last     time.Time
}

// NewThrottle returns a Throttle reading time from now.
func NewThrottle(now func() time.Time) *Throttle {
	return &Throttle{now: now, QueueWait: defaultQueueWait, accounts: map[string]*attempts{}, addrs: map[string]*attempts{},
		byAccount: map[string]int{}, byAddr: map[string]int{}, changed: make(chan struct{})}
}

// Fail records a failed attempt for the account name and client address and
// returns how long the response to this failed attempt should be held.
func (t *Throttle) Fail(account, addr string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	return max(t.bump(0, t.accounts, account, accountGrace, now), t.bump(1, t.addrs, addr, addressGrace, now))
}

// Succeed forgets the failures of the account name and client address.
func (t *Throttle) Succeed(account, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.accounts, account)
	delete(t.addrs, addr)
}

// live reports whether the entry still counts: it has not been idle so long
// that it is forgotten.
func (a *attempts) live(now time.Time) bool { return now.Sub(a.last) <= forgetAfter }

// carriesDelay is whether dropping the entry would shorten someone's delay.
func (a *attempts) carriesDelay(grace int, now time.Time) bool {
	return a.live(now) && delayFor(a.failures, grace) > 0
}

func (t *Throttle) bump(which int, m map[string]*attempts, key string, grace int, now time.Time) time.Duration {
	a := m[key]
	if a != nil && !a.live(now) {
		delete(m, key)
		a = nil
	}
	if a == nil {
		if len(m) >= maxTracked && !t.makeRoom(which, m, grace, now) {
			return 0 // table full of active entries: refuse to track the newcomer
		}
		a = &attempts{}
		m[key] = a
	}
	if a.failures < maxFailures {
		a.failures++
	}
	a.last = now
	return delayFor(a.failures, grace)
}

// makeRoom frees one entry without touching any that carries a delay: expired
// entries first (swept at most once a minute), then the lowest-count entry of
// a bounded sample. It reports whether there is room.
func (t *Throttle) makeRoom(which int, m map[string]*attempts, grace int, now time.Time) bool {
	if now.Sub(t.swept[which]) >= sweepEvery {
		t.swept[which] = now
		for k, a := range m {
			if !a.live(now) {
				delete(m, k)
			}
		}
		if len(m) < maxTracked {
			return true
		}
	}
	victim, best, seen := "", 0, 0
	for k, a := range m {
		if !a.carriesDelay(grace, now) && (victim == "" || a.failures < best) {
			victim, best = k, a.failures
		}
		if seen++; seen >= evictionSample {
			break
		}
	}
	if victim == "" {
		return false
	}
	delete(m, victim)
	return true
}

// delayFor is zero within the grace allowance, then base, 2*base, 4*base ...
// up to the cap.
func delayFor(failures, grace int) time.Duration {
	n := failures - grace
	if n <= 0 {
		return 0
	}
	d := delayBase
	for i := 1; i < n && d < delayCap; i++ {
		d *= 2
	}
	return min(d, delayCap)
}

// Admit reserves a password-verification slot for the account name and client
// address, waiting up to QueueWait for one. ok is false when none came free (or
// ctx ended); the caller then answers without hashing anything. The caller
// must call release on every path.
func (t *Throttle) Admit(ctx context.Context, account, addr string) (release func(), ok bool) {
	var timer <-chan time.Time
	for {
		t.mu.Lock()
		if t.tryTake(account, addr) {
			t.mu.Unlock()
			var once sync.Once
			return func() { once.Do(func() { t.give(account, addr) }) }, true
		}
		if timer == nil {
			if t.waiters >= maxWaiters {
				t.mu.Unlock()
				return nil, false
			}
			t.waiters++
			defer func() { t.mu.Lock(); t.waiters--; t.mu.Unlock() }()
			tm := time.NewTimer(t.QueueWait)
			defer tm.Stop()
			timer = tm.C
		}
		changed := t.changed
		t.mu.Unlock()
		select {
		case <-changed:
		case <-timer:
			return nil, false
		case <-ctx.Done():
			return nil, false
		}
	}
}

// tryTake requires mu to be held.
func (t *Throttle) tryTake(account, addr string) bool {
	limit := maxVerifications - reservedVerifications
	if a := t.addrs[addr]; a == nil || !a.live(t.now()) {
		limit = maxVerifications // a clean address may use the reserved slot
	}
	if t.inUse >= limit || t.byAccount[account] >= perKeyVerifications || t.byAddr[addr] >= perKeyVerifications {
		return false
	}
	t.inUse++
	t.byAccount[account]++
	t.byAddr[addr]++
	return true
}

func (t *Throttle) give(account, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inUse--
	if t.byAccount[account]--; t.byAccount[account] <= 0 {
		delete(t.byAccount, account)
	}
	if t.byAddr[addr]--; t.byAddr[addr] <= 0 {
		delete(t.byAddr, addr)
	}
	close(t.changed)
	t.changed = make(chan struct{})
}

// beginHold reserves one of the bounded places for holding a failed response.
func (t *Throttle) beginHold() (end func(), ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.holds >= maxHolds {
		return nil, false
	}
	t.holds++
	return func() { t.mu.Lock(); t.holds--; t.mu.Unlock() }, true
}
