package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// (a) A correct password is never refused or delayed because of other
// people's failures; only the response to a FAILED attempt is held.

func TestACorrectPasswordIsNeverDelayedByOtherPeoplesFailures(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 40; i++ { // many addresses, one account
		f.login("admin", "wrong "+strconv.Itoa(i), fromAddr("198.51.100."+strconv.Itoa(i%200+1)))
	}
	for i := 0; i < 40; i++ { // one noisy address (a shared proxy), many names
		f.login("guess"+strconv.Itoa(i), "wrong", fromAddr("203.0.113.7"))
	}
	before := len(f.sleeps())
	f.hasher.reset()
	w := f.login("admin", testPassword, fromAddr("203.0.113.7"))
	if w.Code != http.StatusOK {
		t.Fatalf("correct password status=%d", w.Code)
	}
	if len(f.sleeps()) != before {
		t.Fatal("the correct password was held back")
	}
	if f.hasher.verifies != 1 {
		t.Fatalf("verifies=%d", f.hasher.verifies)
	}
}

func TestOnlyFailedAttemptsAreHeldAndTheHoldGrowsToACap(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 30; i++ {
		w := f.login("admin", "wrong "+strconv.Itoa(i), fromAddr("198.51.100."+strconv.Itoa(i+1)))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status=%d, want 401 (a failed guess is answered, after its delay)", i, w.Code)
		}
	}
	got := f.sleeps()
	if len(got) < 6 {
		t.Fatalf("holds=%v", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("hold shrank: %v", got)
		}
	}
	last := got[len(got)-1]
	if last != delayCap || got[len(got)-2] != delayCap {
		t.Fatalf("hold not capped at %v: %v", delayCap, got)
	}
	if delayCap >= 30*time.Second {
		t.Fatalf("delayCap=%v must stay below the server write timeout", delayCap)
	}
	grew := false
	for i := 1; i < len(got); i++ {
		if got[i] >= 2*got[i-1] && got[i] < delayCap {
			grew = true
		}
	}
	if !grew {
		t.Fatalf("hold does not grow exponentially: %v", got)
	}
}

func TestPasswordChangeHoldsWrongCurrentPasswordsButNeverTheRightOne(t *testing.T) {
	f := newFixture(t)
	cs := f.signIn()
	for i := 0; i < 12; i++ {
		w := f.authed(http.MethodPost, "/auth/local/password", changeBody("wrong "+strconv.Itoa(i), goodNewPassword), cs)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status=%d", w.Code)
		}
	}
	if len(f.sleeps()) == 0 {
		t.Fatal("a stolen session can guess the current password without delay")
	}
	before := len(f.sleeps())
	if w := f.authed(http.MethodPost, "/auth/local/password", changeBody(testPassword, goodNewPassword), cs); w.Code != http.StatusNoContent {
		t.Fatalf("right current password refused: %d", w.Code)
	}
	if len(f.sleeps()) != before {
		t.Fatal("right password was held")
	}
}

// gatedHasher blocks any verification whose password starts with "block-"
// until released, and records the highest concurrency it saw.
type gatedHasher struct {
	PasswordHasher
	release  chan struct{}
	entered  chan string
	cur, max atomic.Int32
}

func (g *gatedHasher) Verify(pw, enc string) bool {
	n := g.cur.Add(1)
	for {
		m := g.max.Load()
		if n <= m || g.max.CompareAndSwap(m, n) {
			break
		}
	}
	defer g.cur.Add(-1)
	if strings.HasPrefix(pw, "block-") {
		g.entered <- pw
		<-g.release
	}
	return g.PasswordHasher.Verify(pw, enc)
}

func newGated(f *fixture) *gatedHasher {
	g := &gatedHasher{PasswordHasher: f.hasher, release: make(chan struct{}), entered: make(chan string, 256)}
	f.h.Hasher = g
	f.h.throttle().QueueWait = 30 * time.Millisecond
	return g
}

// (b) Concurrent verifications stay bounded: globally, per address, per account.

func TestConcurrentVerificationsAreBoundedGloballyPerAddressAndPerAccount(t *testing.T) {
	f := newFixture(t)
	g := newGated(f)
	var wg sync.WaitGroup
	codes := make(chan int, 200)
	for i := 0; i < 120; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Many accounts, many addresses: only the global bound applies.
			codes <- f.login("user"+strconv.Itoa(i), "block-"+strconv.Itoa(i), fromAddr("198.51.100."+strconv.Itoa(i%250+1))).Code
		}()
	}
	for i := 0; i < maxVerifications; i++ {
		<-g.entered
	}
	time.Sleep(100 * time.Millisecond) // let every waiter time out
	if n := g.cur.Load(); n > maxVerifications {
		t.Fatalf("concurrent verifications=%d, bound %d", n, maxVerifications)
	}
	close(g.release)
	wg.Wait()
	if g.max.Load() > maxVerifications {
		t.Fatalf("peak=%d", g.max.Load())
	}
	busy := 0
	for len(codes) > 0 {
		if <-codes == http.StatusTooManyRequests {
			busy++
		}
	}
	if busy == 0 {
		t.Fatal("a flood was never turned away")
	}
}

func TestOneAccountAndOneAddressGetOnlyAFewConcurrentGuesses(t *testing.T) {
	for _, tc := range []struct {
		name string
		vary func(i int) (user, addr string)
	}{
		{"one account from many addresses", func(i int) (string, string) { return "admin", "198.51.100." + strconv.Itoa(i+1) }},
		{"one address with many accounts", func(i int) (string, string) { return "user" + strconv.Itoa(i), "198.51.100.9" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			g := newGated(f)
			var wg sync.WaitGroup
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					u, a := tc.vary(i)
					f.login(u, "block-"+strconv.Itoa(i), fromAddr(a))
				}()
			}
			for i := 0; i < perKeyVerifications; i++ {
				<-g.entered
			}
			time.Sleep(100 * time.Millisecond)
			if n := g.cur.Load(); n != perKeyVerifications {
				t.Fatalf("concurrent guesses=%d, want exactly the per-key bound %d", n, perKeyVerifications)
			}
			close(g.release)
			wg.Wait()
		})
	}
}

// Reserved capacity: a flood from addresses that already failed cannot take
// the last verification slot, so a sign-in from a clean address still runs.
func TestReservedCapacityKeepsACorrectSignInPossibleDuringAFlood(t *testing.T) {
	f := newFixture(t)
	g := newGated(f)
	th := f.h.throttle()
	var wg sync.WaitGroup
	for i := 0; i < maxVerifications-reservedVerifications; i++ {
		addr := "198.51.100." + strconv.Itoa(i+1)
		th.Fail("seed"+strconv.Itoa(i), addr) // a dirty address
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.login("flood"+strconv.Itoa(i), "block-x", fromAddr(addr))
		}()
	}
	for i := 0; i < maxVerifications-reservedVerifications; i++ {
		<-g.entered
	}
	// A further flooder from a dirty address is turned away...
	th.Fail("seedx", "198.51.100.200")
	if w := f.login("floodx", "block-y", fromAddr("198.51.100.200")); w.Code != http.StatusTooManyRequests {
		t.Fatalf("flooder status=%d, want 429", w.Code)
	}
	// ...while the real administrator from a clean address gets in.
	if w := f.login("admin", testPassword, fromAddr("192.0.2.50")); w.Code != http.StatusOK {
		t.Fatalf("correct sign-in during a flood status=%d", w.Code)
	}
	close(g.release)
	wg.Wait()
}

// (c) Tracked state is bounded, and eviction never drops an entry that
// still carries a delay.

func TestFloodingMadeUpNamesNeverResetsTheAdminDelay(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}
	th := NewThrottle(clock.Now)
	var last time.Duration
	for i := 0; i < 8; i++ {
		last = th.Fail("admin", "198.51.100.1")
	}
	if last == 0 {
		t.Fatal("admin carries no delay")
	}
	for i := 0; i < maxTracked*2; i++ { // a flood of made-up names, one failure each
		th.Fail("name-"+strconv.Itoa(i), "203.0.113."+strconv.Itoa(i%250))
	}
	next := th.Fail("admin", "198.51.100.2")
	if next < last {
		t.Fatalf("flooding reset admin's delay: before %v, after %v", last, next)
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	if len(th.accounts) > maxTracked || len(th.addrs) > maxTracked {
		t.Fatalf("accounts=%d addrs=%d", len(th.accounts), len(th.addrs))
	}
}

func TestATableFullOfDelayedKeysRefusesNewKeysInsteadOfDroppingActiveOnes(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}
	th := NewThrottle(clock.Now)
	for i := 0; i < maxTracked; i++ {
		for j := 0; j < accountGrace+1; j++ { // each carries a delay
			th.Fail("delayed-"+strconv.Itoa(i), "198.51.100.1")
		}
	}
	th.Fail("newcomer", "198.51.100.1")
	th.mu.Lock()
	if _, ok := th.accounts["newcomer"]; ok {
		t.Fatal("a new key displaced an active one")
	}
	if len(th.accounts) != maxTracked {
		t.Fatalf("accounts=%d", len(th.accounts))
	}
	if th.accounts["delayed-0"] == nil || th.accounts["delayed-"+strconv.Itoa(maxTracked-1)] == nil {
		t.Fatal("an active entry was evicted")
	}
	th.mu.Unlock()

	clock.Advance(forgetAfter + time.Minute) // all expired: room again
	th.Fail("newcomer", "198.51.100.1")
	th.mu.Lock()
	defer th.mu.Unlock()
	if th.accounts["newcomer"] == nil {
		t.Fatal("expired entries were not reclaimed")
	}
}

func TestEvictionPrefersLowestCountEntries(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}
	th := NewThrottle(clock.Now)
	for i := 0; i < maxTracked-1; i++ {
		th.Fail("once-"+strconv.Itoa(i), "198.51.100.1")
	}
	for j := 0; j < 6; j++ {
		th.Fail("admin", "198.51.100.1")
	}
	for i := 0; i < 50; i++ {
		th.Fail("fresh-"+strconv.Itoa(i), "198.51.100.1")
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	if th.accounts["admin"] == nil || th.accounts["admin"].failures < 6 {
		t.Fatal("the high-count entry was evicted")
	}
	if th.accounts["fresh-49"] == nil {
		t.Fatal("a single-failure entry could not be replaced by a newcomer")
	}
}

// (d) The client address.

func TestClientAddressUsesForwardingHeadersOnlyFromTrustedProxies(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.1.5/32")}
	for _, tc := range []struct {
		name    string
		trust   []netip.Prefix
		peer    string
		xff     []string
		want    string
		comment string
	}{
		{"no trust list ignores the header", nil, "10.0.0.2:1", []string{"198.51.100.9"}, "10.0.0.2", ""},
		{"untrusted peer cannot spoof", trusted, "203.0.113.7:1", []string{"1.2.3.4"}, "203.0.113.7", ""},
		{"trusted peer, single hop", trusted, "10.0.0.2:1", []string{"198.51.100.9"}, "198.51.100.9", ""},
		{"rightmost untrusted wins over a spoofed left entry", trusted, "10.0.0.2:1", []string{"1.2.3.4, 198.51.100.9"}, "198.51.100.9", ""},
		{"trusted hops on the right are skipped", trusted, "10.0.0.2:1", []string{"198.51.100.9, 10.0.0.3, 192.168.1.5"}, "198.51.100.9", ""},
		{"several header lines are joined", trusted, "10.0.0.2:1", []string{"1.2.3.4", "198.51.100.9"}, "198.51.100.9", ""},
		{"all trusted falls back to the peer", trusted, "10.0.0.2:1", []string{"10.0.0.9"}, "10.0.0.2", ""},
		{"missing header falls back to the peer", trusted, "10.0.0.2:1", nil, "10.0.0.2", ""},
		{"garbage entry falls back to the peer", trusted, "10.0.0.2:1", []string{"198.51.100.9, not-an-ip"}, "10.0.0.2", ""},
		{"ipv6 client", trusted, "10.0.0.2:1", []string{"2001:db8::1"}, "2001:db8::1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{TrustedProxies: tc.trust}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.peer
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := h.clientAddr(r); got != tc.want {
				t.Fatalf("clientAddr=%q want %q", got, tc.want)
			}
		})
	}
}

func TestBehindATrustedProxyDifferentClientsAreThrottledSeparately(t *testing.T) {
	f := newFixture(t)
	f.h.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	xff := func(ip string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("X-Forwarded-For", ip) }
	}
	for i := 0; i < 12; i++ {
		f.login("guess"+strconv.Itoa(i), "x", xff("198.51.100.5"))
	}
	if h := f.sleeps(); len(h) == 0 || h[len(h)-1] == 0 {
		t.Fatalf("the noisy client was never held: %v", h)
	}
	f.h.throttle().mu.Lock()
	_, noisy := f.h.throttle().addrs["198.51.100.5"]
	_, proxy := f.h.throttle().addrs["203.0.113.7"]
	f.h.throttle().mu.Unlock()
	if !noisy || proxy {
		t.Fatalf("failures were keyed by the proxy (%v) rather than the client (%v)", proxy, noisy)
	}
}

func TestWithoutTrustedProxiesTheForwardingHeaderIsIgnored(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 8; i++ {
		f.login("guess"+strconv.Itoa(i), "x", func(r *http.Request) { r.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i+1)) })
	}
	f.h.throttle().mu.Lock()
	defer f.h.throttle().mu.Unlock()
	if len(f.h.throttle().addrs) != 1 {
		t.Fatalf("tracked %d addresses; the header was believed", len(f.h.throttle().addrs))
	}
}

// the sleep used for held responses honours cancellation.
func TestADefaultHoldEndsWhenTheClientGoesAway(t *testing.T) {
	h := &Handler{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	h.hold(ctx, time.Minute)
	if time.Since(start) > time.Second {
		t.Fatal("hold ignored cancellation")
	}
}
