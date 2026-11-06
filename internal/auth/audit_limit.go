package auth

import (
	"context"
	"sync"
	"time"
)

// The sign-in failure and throttle rows are the only audit rows an outsider
// can make at will, so they are coalesced (D040): at most one row per account
// name or client address per minute, plus a global ceiling per minute. A row
// carries a count of the attempts it stands for. Successes, password changes
// and creation are never coalesced or dropped. The window state is in memory;
// counts reach the row when its window ends (FlushAudit, run every minute by
// RunAuditMaintenance and at shutdown).
const (
	auditWindow           = time.Minute
	maxAuditRowsPerMinute = 60 // per event type, whatever the keys
	maxAuditWindows       = 10000
	auditRetention        = 90 * 24 * time.Hour
	auditPruneEvery       = 24 * time.Hour
	auditFlushEvery       = time.Minute
)

// auditRow is a row written in the current window and the attempts folded into
// it since (pending), not yet added to the stored count.
type auditRow struct {
	id      int64
	start   time.Time
	pending int
}

type auditBucket struct {
	start time.Time
	rows  int
	last  *auditRow
}

type auditLimiter struct {
	mu      sync.Mutex
	byKey   map[string]*auditRow
	rows    map[*auditRow]struct{} // rows of the current window or with a count still to flush
	buckets map[string]*auditBucket
}

func newAuditLimiter() *auditLimiter {
	return &auditLimiter{byKey: map[string]*auditRow{}, rows: map[*auditRow]struct{}{}, buckets: map[string]*auditBucket{}}
}

func (h *Handler) limiter() *auditLimiter {
	h.auditOnce.Do(func() { h.auditLim = newAuditLimiter() })
	return h.auditLim
}

// auditAttempt records a failed or throttled attempt, coalescing flooded ones.
// The account is the normalised name the request used (which may not exist);
// username is set only for a real account.
func (h *Handler) auditAttempt(ctx context.Context, typ, userID, username, account, addr string) error {
	l, now := h.limiter(), h.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	kAcct, kAddr := typ+"\x00a\x00"+account, typ+"\x00r\x00"+addr
	live := func(r *auditRow) bool { return r != nil && now.Sub(r.start) < auditWindow }
	if r := l.byKey[kAcct]; live(r) {
		r.pending++
		l.alias(kAddr, r)
		return nil
	}
	if r := l.byKey[kAddr]; live(r) {
		r.pending++
		l.alias(kAcct, r)
		return nil
	}
	b := l.buckets[typ]
	if b == nil || now.Sub(b.start) >= auditWindow {
		b = &auditBucket{start: now}
		l.buckets[typ] = b
	}
	if b.rows >= maxAuditRowsPerMinute && b.last != nil {
		b.last.pending++
		return nil
	}
	id, err := h.Local.RecordAuthEventID(ctx, AuditEvent{Type: typ, UserID: userID, Username: username, RemoteAddr: addr, At: now.UTC(), Count: 1})
	if err != nil {
		return err
	}
	r := &auditRow{id: id, start: now}
	b.rows++
	b.last = r
	l.rows[r] = struct{}{}
	l.sweep(now)
	l.alias(kAcct, r)
	l.alias(kAddr, r)
	return nil
}

// alias lets a second key share a row's window, unless the table is full.
func (l *auditLimiter) alias(key string, r *auditRow) {
	if _, exists := l.byKey[key]; exists || len(l.byKey) < maxAuditWindows {
		l.byKey[key] = r
	}
}

// sweep forgets key windows that ended; the rows themselves stay in l.rows
// until their count is flushed.
func (l *auditLimiter) sweep(now time.Time) {
	if len(l.byKey) < maxAuditWindows/2 {
		return
	}
	for k, r := range l.byKey {
		if now.Sub(r.start) >= auditWindow {
			delete(l.byKey, k)
		}
	}
}

// FlushAudit adds the attempts folded into rows since they were written to
// those rows, and forgets windows that have ended.
func (h *Handler) FlushAudit(ctx context.Context) {
	if h.Local == nil {
		return
	}
	l, now := h.limiter(), h.now()
	type job struct {
		r *auditRow
		n int
	}
	var jobs []job
	l.mu.Lock()
	for r := range l.rows {
		if r.pending > 0 {
			jobs = append(jobs, job{r, r.pending})
			r.pending = 0
		}
	}
	for k, r := range l.byKey {
		if now.Sub(r.start) >= auditWindow {
			delete(l.byKey, k)
		}
	}
	for r := range l.rows {
		if now.Sub(r.start) >= auditWindow {
			delete(l.rows, r)
		}
	}
	l.mu.Unlock()
	for _, j := range jobs {
		if err := h.Local.AddAuthEventCount(ctx, j.r.id, j.n); err != nil {
			h.warn("could not update the authentication audit")
			l.mu.Lock()
			j.r.pending += j.n // try again at the next flush
			l.rows[j.r] = struct{}{}
			l.mu.Unlock()
		}
	}
}

// maintainAudit flushes counts and prunes rows past retention.
func (h *Handler) maintainAudit(ctx context.Context) {
	h.FlushAudit(ctx)
	if h.Local == nil {
		return
	}
	if _, err := h.Local.PruneAuthEvents(ctx, h.now().Add(-auditRetention)); err != nil {
		h.warn("could not prune the authentication audit")
	}
}

// RunAuditMaintenance prunes old audit rows at start and then daily, and
// flushes coalesced counts every minute, until ctx ends (then flushes once
// more). Run it in its own goroutine.
func (h *Handler) RunAuditMaintenance(ctx context.Context) {
	h.maintainAudit(ctx)
	flush, prune := time.NewTicker(auditFlushEvery), time.NewTicker(auditPruneEvery)
	defer flush.Stop()
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			h.FlushAudit(context.WithoutCancel(ctx))
			return
		case <-flush.C:
			h.FlushAudit(ctx)
		case <-prune.C:
			h.maintainAudit(ctx)
		}
	}
}
