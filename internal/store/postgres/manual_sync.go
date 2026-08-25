package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"git.example.com/evilperson1337/Requestarr/internal/inventory"
)

// ManualSyncRepository records source-local sync intent without modifying source
// configuration, selected libraries, credentials, or configuration audits.
type ManualSyncRepository struct{ pool *Pool }

func NewManualSyncRepository(pool *Pool) *ManualSyncRepository {
	return &ManualSyncRepository{pool: pool}
}

func (r *ManualSyncRepository) RequestManualSync(ctx context.Context, request inventory.ManualSyncRequest) (inventory.ManualSyncRequestResult, error) {
	if err := inventory.ValidateManualSyncRequest(request); err != nil {
		return inventory.ManualSyncRequestResult{}, fmt.Errorf("%w: %v", ErrInventoryPersistenceInvalid, err)
	}
	if err := r.available(ctx); err != nil {
		return inventory.ManualSyncRequestResult{}, err
	}
	request = inventory.CanonicalManualSyncRequest(request)
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return inventory.ManualSyncRequestResult{}, classifyInventoryError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, lockID := range []string{request.SourceID, request.ID} {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockID); err != nil {
			return inventory.ManualSyncRequestResult{}, classifyInventoryError(ctx, err)
		}
	}
	var sourceID, actorID, pendingID string
	var recordedAt time.Time
	err = tx.QueryRow(ctx, `SELECT source_id,COALESCE(actor_id::text,''),requested_at,COALESCE(pending_intent_id::text,'') FROM inventory_manual_sync_request_attempts WHERE request_id=$1 AND disposition IN ('recorded','coalesced')`, request.ID).Scan(&sourceID, &actorID, &recordedAt, &pendingID)
	if err == nil {
		if sourceID != request.SourceID || actorID != request.RequestedBy || !recordedAt.Equal(request.RequestedAt) {
			return inventory.ManualSyncRequestResult{}, inventory.ErrManualSyncRequestConflict
		}
		if _, err = tx.Exec(ctx, `INSERT INTO inventory_manual_sync_request_attempts(request_id,source_id,actor_id,requested_at,disposition,pending_intent_id) VALUES($1,$2,NULLIF($3,'')::uuid,$4,'replayed',$5::uuid)`, request.ID, request.SourceID, request.RequestedBy, request.RequestedAt, pendingID); err != nil {
			return inventory.ManualSyncRequestResult{}, classifyInventoryError(ctx, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return inventory.ManualSyncRequestResult{}, classifyInventoryError(ctx, err)
		}
		return inventory.ManualSyncRequestResult{Disposition: inventory.ManualSyncReplayed, PendingIntentID: pendingID}, nil
	}
	if err != pgx.ErrNoRows {
		return inventory.ManualSyncRequestResult{}, classifyInventoryError(ctx, err)
	}
	var kind string
	var enabled bool
	var baseURL, secretRef *string
	err = tx.QueryRow(ctx, `SELECT kind,enabled,base_url,secret_ref FROM library_sources WHERE id=$1 FOR UPDATE`, request.SourceID).Scan(&kind, &enabled, &baseURL, &secretRef)
	if err == pgx.ErrNoRows {
		return inventory.ManualSyncRequestResult{}, inventory.ErrManualSyncSourceNotFound
	}
	if err != nil {
		return inventory.ManualSyncRequestResult{}, classifyInventoryError(ctx, err)
	}
	if kind != string(inventory.SourceAudiobookshelf) {
		return inventory.ManualSyncRequestResult{}, inventory.ErrManualSyncSourceWrongKind
	}
	if !enabled {
		return inventory.ManualSyncRequestResult{}, inventory.ErrManualSyncSourceDisabled
	}
	var configured bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_source_configurations WHERE source_id=$1)`, request.SourceID).Scan(&configured)
	if err != nil {
		return inventory.ManualSyncRequestResult{}, classifyInventoryError(ctx, err)
	}
	if !configured || baseURL != nil || secretRef != nil {
		return inventory.ManualSyncRequestResult{}, inventory.ErrManualSyncSourceUnconfigured
	}
	var disposition = inventory.ManualSyncRecorded
	err = tx.QueryRow(ctx, `SELECT id FROM inventory_manual_sync_intents WHERE source_id=$1 AND state='pending' FOR UPDATE`, request.SourceID).Scan(&pendingID)
	if err == pgx.ErrNoRows {
		pendingID = request.ID
		if _, err = tx.Exec(ctx, `INSERT INTO inventory_manual_sync_intents(id,source_id,requested_by,requested_at,state) VALUES($1,$2,NULLIF($3,'')::uuid,$4,'pending')`, pendingID, request.SourceID, request.RequestedBy, request.RequestedAt); err != nil {
			return inventory.ManualSyncRequestResult{}, classifyInventoryError(ctx, err)
		}
	} else if err == nil {
		disposition = inventory.ManualSyncCoalesced
	} else {
		return inventory.ManualSyncRequestResult{}, classifyInventoryError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO inventory_manual_sync_request_attempts(request_id,source_id,actor_id,requested_at,disposition,pending_intent_id) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6::uuid)`, request.ID, request.SourceID, request.RequestedBy, request.RequestedAt, disposition, pendingID); err != nil {
		return inventory.ManualSyncRequestResult{}, classifyInventoryError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return inventory.ManualSyncRequestResult{}, classifyInventoryError(ctx, err)
	}
	return inventory.ManualSyncRequestResult{Disposition: disposition, PendingIntentID: pendingID}, nil
}

func (r *ManualSyncRepository) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrInventoryPersistenceUnavailable, err)
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return ErrInventoryPersistenceUnavailable
	}
	return nil
}

var _ inventory.ManualSyncRequestRepository = (*ManualSyncRepository)(nil)
