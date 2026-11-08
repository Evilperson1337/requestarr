package inventory

import (
	"context"
	"fmt"
	"sync"
)

// A sync can fail in the middle of a long enumeration, and the adapter's error
// says what went wrong but not where. An adapter reports where it is with
// SetStep; the sync job reads the last step from its StepTracker when the
// snapshot fails and logs it. A step is a fixed phrase plus numbers (a page
// offset), never a title, an identifier from the library or any provider text.

type stepKey struct{}

// StepTracker holds the most recent step an adapter reported.
type StepTracker struct {
	mu   sync.Mutex
	step string
}

// WithStepTracker returns ctx carrying a tracker that SetStep fills in.
func WithStepTracker(ctx context.Context) (context.Context, *StepTracker) {
	t := &StepTracker{}
	return context.WithValue(ctx, stepKey{}, t), t
}

// SetStep records what the adapter is doing now. It does nothing when ctx
// carries no tracker, so adapters call it unconditionally.
func SetStep(ctx context.Context, format string, args ...any) {
	if t, ok := ctx.Value(stepKey{}).(*StepTracker); ok {
		t.mu.Lock()
		t.step = fmt.Sprintf(format, args...)
		t.mu.Unlock()
	}
}

// Current is the last step reported, or "" when none was.
func (t *StepTracker) Current() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.step
}
