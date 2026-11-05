package notifications

import (
	"context"
	"fmt"
	"time"
)

type Delivery struct {
	ID, UserID, Target, Title, Body, LeaseToken string
	Attempts                                    int
}
type Outbox interface {
	ClaimGlobalApprise(context.Context, time.Duration) (id, title, body, token string, attempts int, err error)
	CompleteGlobalApprise(context.Context, string, string, bool, time.Duration) error
}

// RunWorker polls the durable outbox, using leases for cross-process exclusion
// and capped exponential retries. Apprise is a single operator-configured target.
// A sender that also has Configured() bool is asked before each claim, so an
// Apprise key cleared at runtime leaves events pending rather than failing them.
func RunWorker(ctx context.Context, outbox Outbox, sender interface {
	Send(context.Context, Message) error
}) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		// A sender that is switched off (its credential was cleared at runtime)
		// leaves events pending in the outbox instead of failing and retrying them.
		if g, ok := sender.(interface{ Configured() bool }); ok && !g.Configured() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
			continue
		}
		id, title, body, token, attempts, err := outbox.ClaimGlobalApprise(ctx, 30*time.Second)
		if err != nil {
			return fmt.Errorf("claim notification delivery: %w", err)
		}
		if id != "" {
			deliverCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err = sender.Send(deliverCtx, Message{Title: title, Body: body})
			cancel()
			delay := retryDelay(attempts)
			if completeErr := outbox.CompleteGlobalApprise(ctx, id, token, err == nil, delay); completeErr != nil {
				return fmt.Errorf("complete notification delivery: %w", completeErr)
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func retryDelay(attempts int) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	if attempts > 12 {
		return time.Hour
	}
	return time.Second * time.Duration(1<<attempts)
}
