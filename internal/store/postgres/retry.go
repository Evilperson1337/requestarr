package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// connectError is a failed connection, described in words that are safe to log.
// transient says whether waiting could help: a database that is not up yet
// (refused, starting, slow, or a name that is not resolvable yet) may come up,
// while a rejected login or a missing database never will.
type connectError struct {
	message   string
	transient bool
}

func (e *connectError) Error() string { return e.message }

// IsTransient reports whether err is a connection failure that waiting could fix.
func IsTransient(err error) bool {
	var c *connectError
	return errors.As(err, &c) && c.transient
}

// RetryPolicy is how startup waits for a database that is not ready yet.
type RetryPolicy struct {
	// Attempts is the total number of tries, so 3 means two pauses. Less than 1 means 1.
	Attempts int
	// Wait is the fixed pause between tries; there is none after the last one.
	Wait time.Duration
	// OnRetry runs before each pause with the try that just failed (1-based).
	OnRetry func(attempt, attempts int, wait time.Duration, err error)

	sleep func(context.Context, time.Duration) error // replaced in tests
}

func (p RetryPolicy) pause(ctx context.Context) error {
	if p.sleep != nil {
		return p.sleep(ctx, p.Wait)
	}
	timer := time.NewTimer(p.Wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retry runs attempt until it works, fails for a cause waiting cannot fix, runs
// out of tries, or ctx ends (so stopping the server never waits out a pause).
func retry[T any](ctx context.Context, p RetryPolicy, attempt func(context.Context) (T, error)) (T, error) {
	var zero T
	attempts := p.Attempts
	if attempts < 1 {
		attempts = 1
	}
	for n := 1; ; n++ {
		value, err := attempt(ctx)
		if err == nil {
			return value, nil
		}
		if !IsTransient(err) {
			return zero, err
		}
		if n >= attempts {
			if attempts > 1 {
				return zero, fmt.Errorf("%w (gave up after %d attempts, %s apart)", err, attempts, p.Wait)
			}
			return zero, err
		}
		if p.OnRetry != nil {
			p.OnRetry(n, attempts, p.Wait, err)
		}
		if err := p.pause(ctx); err != nil {
			return zero, err
		}
	}
}

// OpenWithRetry is Open for a server start: it retries only the connection, so a
// database that is still coming up is waited for, and runs the migrations once.
func OpenWithRetry(ctx context.Context, databaseURL string, policy RetryPolicy) (*Store, error) {
	pool, err := retry(ctx, policy, func(ctx context.Context) (*pgxpool.Pool, error) {
		return newPool(ctx, databaseURL)
	})
	if err != nil {
		return nil, err
	}
	if err := runMigrations(pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}
