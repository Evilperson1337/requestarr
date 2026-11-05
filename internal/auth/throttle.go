package auth

import (
	"sync"
	"time"
)

// The sign-in delay (D040). Failed attempts slow the next ones down, per
// account name and per client address, doubling up to a cap. Nothing locks an
// account: once the delay has been served the right password works. State is
// in memory in the one process, so a restart clears it.
const (
	delayBase    = time.Second
	delayCap     = 5 * time.Minute
	accountGrace = 2 // typos allowed per account before the first delay
	addressGrace = 5 // allowed per client address; looser, because a reverse proxy can put everyone behind one address
	forgetAfter  = time.Hour
	maxTracked   = 10000
	maxFailures  = 64 // keeps the doubling in delayFor bounded
)

// Throttle tracks failed attempts. The zero value is not usable; use NewThrottle.
type Throttle struct {
	now func() time.Time

	mu       sync.Mutex
	accounts map[string]*attempts
	addrs    map[string]*attempts
}

type attempts struct {
	failures int
	until    time.Time // no attempt is allowed before this
	last     time.Time
}

// NewThrottle returns a Throttle reading time from now.
func NewThrottle(now func() time.Time) *Throttle {
	return &Throttle{now: now, accounts: map[string]*attempts{}, addrs: map[string]*attempts{}}
}

// Wait is how long the caller must wait before this account name or client
// address may try again; zero means go ahead. It does not count as an attempt.
func (t *Throttle) Wait(account, addr string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	var wait time.Duration
	for _, a := range []*attempts{t.accounts[account], t.addrs[addr]} {
		if a != nil && a.until.After(now) && a.until.Sub(now) > wait {
			wait = a.until.Sub(now)
		}
	}
	return wait
}

// Fail records a failed attempt for the account name and client address.
func (t *Throttle) Fail(account, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	bump(t.accounts, account, accountGrace, now)
	bump(t.addrs, addr, addressGrace, now)
}

// Succeed forgets the failures of the account name and client address.
func (t *Throttle) Succeed(account, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.accounts, account)
	delete(t.addrs, addr)
}

func bump(m map[string]*attempts, key string, grace int, now time.Time) {
	a := m[key]
	if a != nil && now.Sub(a.last) > forgetAfter {
		a = nil
	}
	if a == nil {
		if _, exists := m[key]; !exists && len(m) >= maxTracked {
			evict(m, now)
		}
		a = &attempts{}
		m[key] = a
	}
	if a.failures < maxFailures {
		a.failures++
	}
	a.last = now
	a.until = now.Add(delayFor(a.failures, grace))
}

// evict makes room, so a flood of made-up names cannot grow the map without
// bound: idle entries go first, then arbitrary ones.
func evict(m map[string]*attempts, now time.Time) {
	for k, a := range m {
		if now.Sub(a.last) > forgetAfter {
			delete(m, k)
		}
	}
	for k := range m {
		if len(m) < maxTracked {
			return
		}
		delete(m, k)
	}
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
