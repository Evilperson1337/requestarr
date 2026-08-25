package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/reconcile"
)

var (
	ErrReconciliationUnavailable = errors.New("reconciliation evidence repository unavailable")
	ErrReconciliationInvalid     = errors.New("reconciliation evidence input is invalid")
	ErrReconciliationProvenance  = errors.New("reconciliation evidence provenance not found")
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// ReconciliationEvidenceRepository persists only normalized, bounded evidence.
// Each actual activation/deactivation is atomically accompanied by one audit row.
type ReconciliationEvidenceRepository struct{ pool *Pool }

func NewReconciliationEvidenceRepository(pool *Pool) *ReconciliationEvidenceRepository {
	return &ReconciliationEvidenceRepository{pool: pool}
}

func (r *ReconciliationEvidenceRepository) Activate(ctx context.Context, activation reconcile.Activation) (reconcile.Change, error) {
	if err := validateActivation(activation); err != nil {
		return reconcile.Change{}, err
	}
	if err := ctx.Err(); err != nil {
		return reconcile.Change{}, fmt.Errorf("%w: %v", reconcile.ErrCanceled, err)
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return reconcile.Change{}, ErrReconciliationUnavailable
	}
	evidence, _ := domain.NormalizeMatchEvidence(activation.Evidence)
	attributes, err := json.Marshal(evidence.Attributes)
	if err != nil { // normalized attributes are JSON primitives, but do not risk a partial write.
		return reconcile.Change{}, fmt.Errorf("%w: encode attributes", ErrReconciliationInvalid)
	}
	review := "not_required"
	if evidence.Confidence == domain.MatchPossible {
		review = "pending"
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return reconcile.Change{}, classifyReconciliationError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// Serialize the one active request/inventory association. This avoids a
	// uniqueness race while retaining superseded evidence as inactive history.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2, 0))`, activation.Target.RequestID, activation.Target.InventoryItemID); err != nil {
		return reconcile.Change{}, classifyReconciliationError(ctx, err)
	}
	var existingID, existingSource, existingMethod, existingConfidence, existingReason, existingEvidence, existingReview string
	err = tx.QueryRow(ctx, `SELECT id,source_id,method,confidence,reason,evidence::text,review_state
 FROM reconciliation_match_evidence WHERE request_id=$1 AND inventory_item_id=$2 AND active FOR UPDATE`, activation.Target.RequestID, activation.Target.InventoryItemID).
		Scan(&existingID, &existingSource, &existingMethod, &existingConfidence, &existingReason, &existingEvidence, &existingReview)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return reconcile.Change{}, classifyReconciliationError(ctx, err)
	}
	if err == nil && existingSource == activation.Target.SourceID && existingMethod == string(evidence.Method) && existingConfidence == string(evidence.Confidence) && existingReason == evidence.Reason && existingEvidence == string(attributes) && existingReview == review {
		if err := tx.Commit(ctx); err != nil {
			return reconcile.Change{}, classifyReconciliationError(ctx, err)
		}
		return reconcile.Change{AssociationID: existingID}, nil
	}
	if err == nil {
		if _, err = tx.Exec(ctx, `UPDATE reconciliation_match_evidence SET active=FALSE,deactivated_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, existingID); err != nil {
			return reconcile.Change{}, classifyReconciliationError(ctx, err)
		}
		eventID, eventErr := newUUID()
		if eventErr != nil {
			return reconcile.Change{}, fmt.Errorf("%w: generate audit ID", ErrReconciliationUnavailable)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO reconciliation_match_events(id,evidence_id,event_type,created_at) VALUES ($1,$2,'deactivated',clock_timestamp())`, eventID, existingID); err != nil {
			return reconcile.Change{}, classifyReconciliationError(ctx, err)
		}
	}
	id, err := newUUID()
	if err != nil {
		return reconcile.Change{}, fmt.Errorf("%w: generate evidence ID", ErrReconciliationUnavailable)
	}
	var evidenceID string
	err = tx.QueryRow(ctx, `INSERT INTO reconciliation_match_evidence
 (id, request_id, source_id, inventory_item_id, method, confidence, reason, evidence, review_state, active, observed_at, created_at, updated_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,TRUE,$10,clock_timestamp(),clock_timestamp())
 RETURNING id`, id, activation.Target.RequestID, activation.Target.SourceID, activation.Target.InventoryItemID,
		evidence.Method, evidence.Confidence, evidence.Reason, attributes, review, activation.ObservedAt.UTC()).Scan(&evidenceID)
	if err != nil {
		return reconcile.Change{}, classifyReconciliationError(ctx, err)
	}
	eventID, err := newUUID()
	if err != nil {
		return reconcile.Change{}, fmt.Errorf("%w: generate audit ID", ErrReconciliationUnavailable)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO reconciliation_match_events(id,evidence_id,event_type,created_at) VALUES ($1,$2,'activated',clock_timestamp())`, eventID, evidenceID); err != nil {
		return reconcile.Change{}, classifyReconciliationError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return reconcile.Change{}, classifyReconciliationError(ctx, err)
	}
	return reconcile.Change{AssociationID: evidenceID, Changed: true}, nil
}

func (r *ReconciliationEvidenceRepository) Deactivate(ctx context.Context, target reconcile.Target, at time.Time) (reconcile.Change, error) {
	if err := validateTarget(target); err != nil || at.IsZero() {
		return reconcile.Change{}, fmt.Errorf("%w: target and deactivation time are required", ErrReconciliationInvalid)
	}
	if err := ctx.Err(); err != nil {
		return reconcile.Change{}, fmt.Errorf("%w: %v", reconcile.ErrCanceled, err)
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return reconcile.Change{}, ErrReconciliationUnavailable
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return reconcile.Change{}, classifyReconciliationError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2, 0))`, target.RequestID, target.InventoryItemID); err != nil {
		return reconcile.Change{}, classifyReconciliationError(ctx, err)
	}
	var evidenceID string
	err = tx.QueryRow(ctx, `UPDATE reconciliation_match_evidence SET active=FALSE, deactivated_at=$4, updated_at=clock_timestamp()
 WHERE request_id=$1 AND source_id=$2 AND inventory_item_id=$3 AND active RETURNING id`, target.RequestID, target.SourceID, target.InventoryItemID, at.UTC()).Scan(&evidenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return reconcile.Change{}, classifyReconciliationError(ctx, err)
		}
		return reconcile.Change{}, nil
	}
	if err != nil {
		return reconcile.Change{}, classifyReconciliationError(ctx, err)
	}
	eventID, err := newUUID()
	if err != nil {
		return reconcile.Change{}, fmt.Errorf("%w: generate audit ID", ErrReconciliationUnavailable)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO reconciliation_match_events(id,evidence_id,event_type,created_at) VALUES ($1,$2,'deactivated',clock_timestamp())`, eventID, evidenceID); err != nil {
		return reconcile.Change{}, classifyReconciliationError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return reconcile.Change{}, classifyReconciliationError(ctx, err)
	}
	return reconcile.Change{AssociationID: evidenceID, Changed: true}, nil
}

func validateActivation(activation reconcile.Activation) error {
	if err := validateTarget(activation.Target); err != nil || activation.ObservedAt.IsZero() {
		return fmt.Errorf("%w: target and observed time are required", ErrReconciliationInvalid)
	}
	if _, err := domain.NormalizeMatchEvidence(activation.Evidence); err != nil {
		return fmt.Errorf("%w: %v", ErrReconciliationInvalid, err)
	}
	return nil
}
func validateTarget(target reconcile.Target) error {
	for _, id := range []string{target.RequestID, target.SourceID, target.InventoryItemID} {
		if !uuidPattern.MatchString(id) {
			return ErrReconciliationInvalid
		}
	}
	return nil
}
func classifyReconciliationError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %v", reconcile.ErrCanceled, ctx.Err())
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrReconciliationProvenance
	}
	return fmt.Errorf("%w: %v", ErrReconciliationUnavailable, err)
}
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:]), nil
}
