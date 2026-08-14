package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	startupTimeout   = 10 * time.Second
	readinessTimeout = 2 * time.Second
)

// Pool is the single application-owned PostgreSQL connection pool.
type Pool struct {
	pool *pgxpool.Pool
}

// Open constructs and initially pings a pool within one bounded startup phase.
func Open(ctx context.Context, databaseURL string) (*Pool, error) {
	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("database pool configuration invalid")
	}
	config.ConnConfig.RuntimeParams["application_name"] = "requestarr"

	pool, err := pgxpool.NewWithConfig(startupCtx, config)
	if err != nil {
		return nil, errors.New("database pool open failed")
	}
	if err := pool.Ping(startupCtx); err != nil {
		pool.Close()
		return nil, errors.New("database initial ping failed")
	}

	return &Pool{pool: pool}, nil
}

// Ping reports whether PostgreSQL is reachable.
func (p *Pool) Ping(ctx context.Context) error {
	if p == nil || p.pool == nil {
		return errors.New("database pool unavailable")
	}
	return p.pool.Ping(ctx)
}

// Ready performs the bounded check consumed by the HTTP readiness probe.
func (p *Pool) Ready(ctx context.Context) error {
	readyCtx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()
	return p.Ping(readyCtx)
}

// Close releases all pool connections.
func (p *Pool) Close() {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
}
