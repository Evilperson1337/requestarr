package postgres

import (
	"context"
	"errors"

	"git.example.com/evilperson1337/Requestarr/internal/setup"
)

const setupStatusQuery = `SELECT complete, current_step
FROM setup_state
WHERE singleton = TRUE`

// SetupStatusRepository reads the one authoritative setup_state singleton.
type SetupStatusRepository struct {
	pool *Pool
}

func NewSetupStatusRepository(pool *Pool) *SetupStatusRepository {
	return &SetupStatusRepository{pool: pool}
}

// ReadState reads at most one valid state. Cardinality and scan failures are
// explicit invalid-state errors and database details never cross the boundary.
func (r *SetupStatusRepository) ReadState(ctx context.Context) (setup.State, error) {
	if err := ctx.Err(); err != nil {
		return setup.State{}, setup.ErrStatusCanceled
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return setup.State{}, setup.ErrStatusUnavailable
	}

	rows, err := r.pool.pool.Query(ctx, setupStatusQuery)
	if err != nil {
		return setup.State{}, classifySetupReadError(ctx, err)
	}
	defer rows.Close()

	var state setup.State
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			return setup.State{}, setup.ErrStateDuplicate
		}
		if err := rows.Scan(&state.Complete, &state.Current); err != nil {
			return setup.State{}, setup.ErrStateInvalid
		}
	}
	if err := rows.Err(); err != nil {
		return setup.State{}, classifySetupReadError(ctx, err)
	}
	if count == 0 {
		return setup.State{}, setup.ErrStateMissing
	}
	if err := state.Validate(); err != nil {
		return setup.State{}, setup.ErrStateInvalid
	}
	return state, nil
}

func classifySetupReadError(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return setup.ErrStatusCanceled
	}
	return setup.ErrStatusUnavailable
}
