package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"git.example.com/evilperson1337/Requestarr/internal/inventory"
)

var (
	ErrInventoryPersistenceUnavailable = errors.New("inventory persistence unavailable")
	ErrInventoryPersistenceInvalid     = errors.New("inventory persistence input is invalid")
	ErrInventorySourceNotFound         = errors.New("inventory source not found")
	ErrInventoryStatusConflict         = errors.New("inventory source status conflict")
	ErrInventorySourceLegacy           = errors.New("inventory source cannot be repurposed")
)

// InventoryRepository persists generic authoritative-cycle audit data and the
// source-local operational status projection. It does not alter typed inventory
// projections, including audiobook snapshots.
type InventoryRepository struct{ pool *Pool }

func NewInventoryRepository(pool *Pool) *InventoryRepository { return &InventoryRepository{pool: pool} }

func (r *InventoryRepository) CommitAuthoritativeCycle(ctx context.Context, cycle inventory.AuthoritativeCycle) (inventory.CycleCommit, error) {
	if err := inventory.ValidateAuthoritativeCycle(cycle); err != nil {
		return inventory.CycleCommit{}, fmt.Errorf("%w: %v", ErrInventoryPersistenceInvalid, err)
	}
	if err := r.available(ctx); err != nil {
		return inventory.CycleCommit{}, err
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return inventory.CycleCommit{}, classifyInventoryError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, cycle.ID); err != nil {
		return inventory.CycleCommit{}, classifyInventoryError(ctx, err)
	}
	var existingSource string
	var existingStarted, existingCompleted time.Time
	err = tx.QueryRow(ctx, `SELECT source_id,started_at,completed_at FROM inventory_authoritative_cycles WHERE id=$1`, cycle.ID).Scan(&existingSource, &existingStarted, &existingCompleted)
	if err == nil {
		if existingSource != cycle.SourceID || !existingStarted.Equal(cycle.StartedAt.UTC()) || !existingCompleted.Equal(cycle.CompletedAt.UTC()) {
			return inventory.CycleCommit{}, ErrInventoryPersistenceInvalid
		}
		rows, err := tx.Query(ctx, `SELECT source_item_id,change_kind,media_type FROM inventory_authoritative_cycle_events WHERE cycle_id=$1 ORDER BY source_item_id`, cycle.ID)
		if err != nil {
			return inventory.CycleCommit{}, classifyInventoryError(ctx, err)
		}
		defer rows.Close()
		existingEvents := make([]inventory.InventoryEvent, 0, len(cycle.Events))
		for rows.Next() {
			var event inventory.InventoryEvent
			if err := rows.Scan(&event.SourceItemID, &event.Kind, &event.MediaType); err != nil {
				return inventory.CycleCommit{}, classifyInventoryError(ctx, err)
			}
			existingEvents = append(existingEvents, event)
		}
		if err := rows.Err(); err != nil {
			return inventory.CycleCommit{}, classifyInventoryError(ctx, err)
		}
		if !sameCycleEvents(existingEvents, cycle.Events) {
			return inventory.CycleCommit{}, ErrInventoryPersistenceInvalid
		}
		if err = tx.Commit(ctx); err != nil {
			return inventory.CycleCommit{}, classifyInventoryError(ctx, err)
		}
		return inventory.CycleCommit{CycleID: cycle.ID}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return inventory.CycleCommit{}, classifyInventoryError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO inventory_authoritative_cycles(id,source_id,started_at,completed_at,committed_at) VALUES($1,$2,$3,$4,clock_timestamp())`, cycle.ID, cycle.SourceID, cycle.StartedAt.UTC(), cycle.CompletedAt.UTC()); err != nil {
		return inventory.CycleCommit{}, classifyInventoryError(ctx, err)
	}
	for _, event := range cycle.Events {
		if _, err = tx.Exec(ctx, `INSERT INTO inventory_authoritative_cycle_events(cycle_id,source_item_id,change_kind,media_type) VALUES($1,$2,$3,$4)`, cycle.ID, event.SourceItemID, event.Kind, event.MediaType); err != nil {
			return inventory.CycleCommit{}, classifyInventoryError(ctx, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return inventory.CycleCommit{}, classifyInventoryError(ctx, err)
	}
	return inventory.CycleCommit{CycleID: cycle.ID, Committed: true}, nil
}

func sameCycleEvents(existing, supplied []inventory.InventoryEvent) bool {
	if len(existing) != len(supplied) {
		return false
	}
	want := make(map[string]inventory.InventoryEvent, len(supplied))
	for _, event := range supplied {
		want[event.SourceItemID] = event
	}
	for _, event := range existing {
		candidate, ok := want[event.SourceItemID]
		if !ok || candidate.Kind != event.Kind || candidate.MediaType != event.MediaType {
			return false
		}
	}
	return true
}

func (r *InventoryRepository) TransitionSourceStatus(ctx context.Context, transition inventory.SourceStatusTransition) (inventory.SourceStatusTransitionResult, error) {
	if err := inventory.ValidateSourceStatusTransition(transition); err != nil {
		return inventory.SourceStatusTransitionResult{}, fmt.Errorf("%w: %v", ErrInventoryPersistenceInvalid, err)
	}
	if err := r.available(ctx); err != nil {
		return inventory.SourceStatusTransitionResult{}, err
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return inventory.SourceStatusTransitionResult{}, classifyInventoryError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, transition.SourceID); err != nil {
		return inventory.SourceStatusTransitionResult{}, classifyInventoryError(ctx, err)
	}
	var priorFrom, priorTo, priorFailure string
	var priorOccurred time.Time
	err = tx.QueryRow(ctx, `SELECT from_state,to_state,occurred_at,failure_detail FROM inventory_source_status_transitions WHERE source_id=$1 AND cycle_id=$2`, transition.SourceID, transition.CycleID).Scan(&priorFrom, &priorTo, &priorOccurred, &priorFailure)
	if err == nil {
		if priorFrom != string(transition.From) || priorTo != string(transition.To) || !priorOccurred.Equal(transition.OccurredAt.UTC()) || priorFailure != transition.FailureDetail {
			return inventory.SourceStatusTransitionResult{}, ErrInventoryPersistenceInvalid
		}
		current, err := scanSourceStatus(ctx, tx, transition.SourceID)
		if err != nil {
			return inventory.SourceStatusTransitionResult{}, classifyInventoryError(ctx, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return inventory.SourceStatusTransitionResult{}, classifyInventoryError(ctx, err)
		}
		return inventory.SourceStatusTransitionResult{Current: current}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return inventory.SourceStatusTransitionResult{}, classifyInventoryError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO inventory_source_status(source_id,state) VALUES($1,'idle') ON CONFLICT(source_id) DO NOTHING`, transition.SourceID); err != nil {
		return inventory.SourceStatusTransitionResult{}, classifyInventoryError(ctx, err)
	}
	current, err := scanSourceStatus(ctx, tx, transition.SourceID)
	if err != nil {
		return inventory.SourceStatusTransitionResult{}, classifyInventoryError(ctx, err)
	}
	if current.State != transition.From {
		return inventory.SourceStatusTransitionResult{}, ErrInventoryStatusConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO inventory_source_status_transitions(source_id,cycle_id,from_state,to_state,occurred_at,failure_detail,applied_at) VALUES($1,$2,$3,$4,$5,$6,clock_timestamp())`, transition.SourceID, transition.CycleID, transition.From, transition.To, transition.OccurredAt.UTC(), transition.FailureDetail); err != nil {
		return inventory.SourceStatusTransitionResult{}, classifyInventoryError(ctx, err)
	}
	if err = updateSourceStatus(ctx, tx, transition); err != nil {
		return inventory.SourceStatusTransitionResult{}, classifyInventoryError(ctx, err)
	}
	current, err = scanSourceStatus(ctx, tx, transition.SourceID)
	if err != nil {
		return inventory.SourceStatusTransitionResult{}, classifyInventoryError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return inventory.SourceStatusTransitionResult{}, classifyInventoryError(ctx, err)
	}
	return inventory.SourceStatusTransitionResult{Applied: true, Current: current}, nil
}

func updateSourceStatus(ctx context.Context, tx pgx.Tx, transition inventory.SourceStatusTransition) error {
	switch transition.To {
	case inventory.SourceSyncRunning:
		_, err := tx.Exec(ctx, `UPDATE inventory_source_status SET state='running',last_started_at=$2,last_completed_at=NULL,failure_detail='' WHERE source_id=$1`, transition.SourceID, transition.OccurredAt.UTC())
		return err
	case inventory.SourceSyncSucceeded:
		_, err := tx.Exec(ctx, `UPDATE inventory_source_status SET state='succeeded',last_completed_at=$2,last_successful_at=$2,failure_detail='' WHERE source_id=$1`, transition.SourceID, transition.OccurredAt.UTC())
		return err
	case inventory.SourceSyncFailed:
		_, err := tx.Exec(ctx, `UPDATE inventory_source_status SET state='failed',last_completed_at=$2,failure_detail=$3 WHERE source_id=$1`, transition.SourceID, transition.OccurredAt.UTC(), transition.FailureDetail)
		return err
	}
	return ErrInventoryPersistenceInvalid
}

func scanSourceStatus(ctx context.Context, tx pgx.Tx, sourceID string) (status inventory.SourceStatus, err error) {
	var started, completed, succeeded *time.Time
	var state string
	err = tx.QueryRow(ctx, `SELECT source_id,state,last_started_at,last_completed_at,last_successful_at,failure_detail FROM inventory_source_status WHERE source_id=$1 FOR UPDATE`, sourceID).Scan(&status.SourceID, &state, &started, &completed, &succeeded, &status.FailureDetail)
	status.State = inventory.SourceSyncState(state)
	if started != nil {
		status.LastStartedAt = *started
	}
	if completed != nil {
		status.LastCompletedAt = *completed
	}
	if succeeded != nil {
		status.LastSuccessfulAt = *succeeded
	}
	return status, err
}

func (r *InventoryRepository) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrInventoryPersistenceUnavailable, err)
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return ErrInventoryPersistenceUnavailable
	}
	return nil
}

func classifyInventoryError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %v", ErrInventoryPersistenceUnavailable, ctx.Err())
	}
	return ErrInventoryPersistenceUnavailable
}

var _ inventory.AuthoritativeCycleRepository = (*InventoryRepository)(nil)
var _ inventory.SourceStatusRepository = (*InventoryRepository)(nil)
