package auth

import (
	"strconv"
	"testing"
	"time"
)

func TestThrottleMemoryStaysBoundedUnderAFloodOfMadeUpNames(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}
	th := NewThrottle(clock.Now)
	for i := 0; i < maxTracked+500; i++ {
		th.Fail("name-"+strconv.Itoa(i), "198.51.100."+strconv.Itoa(i))
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	if len(th.accounts) > maxTracked || len(th.addrs) > maxTracked {
		t.Fatalf("accounts=%d addrs=%d", len(th.accounts), len(th.addrs))
	}
}

func TestDelayCurve(t *testing.T) {
	for _, tc := range []struct {
		failures, grace int
		want            time.Duration
	}{
		{1, 2, 0}, {2, 2, 0}, {3, 2, time.Second}, {4, 2, 2 * time.Second}, {5, 2, 4 * time.Second},
		{12, 2, 300 * time.Second}, {64, 2, 300 * time.Second}, {5, 5, 0}, {6, 5, time.Second},
	} {
		if got := delayFor(tc.failures, tc.grace); got != tc.want {
			t.Errorf("delayFor(%d,%d)=%v want %v", tc.failures, tc.grace, got, tc.want)
		}
	}
}
