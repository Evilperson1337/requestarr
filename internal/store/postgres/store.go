// Package postgres is the first concrete persistence adapter: it owns the
// connection pool, applies embedded migrations at startup, and answers
// readiness checks. Domain-specific repositories belong here in later phases;
// this package intentionally does not know about requests, users, or catalog
// items yet.
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

// Open opens a connection pool, confirms the database is reachable, and
// applies all pending embedded migrations. The returned Store is ready for
// use once Open returns without error.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := newPool(ctx, databaseURL)
	if err != nil {
		return nil, err
	}

	if err := runMigrations(pool); err != nil {
		pool.Close()
		return nil, err
	}

	return &Store{pool: pool}, nil
}

// Ready reports whether the database is currently reachable. It satisfies
// httpapi.ReadinessChecker without internal/httpapi needing to import this
// package's concrete types.
func (s *Store) Ready(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) Close() {
	s.pool.Close()
}
