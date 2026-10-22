package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/jackc/pgx/v5"
)

// suggestMatches is deliberately separate from recordPresence: discovering an
// inexact candidate must never create availability evidence.
func suggestMatches(ctx context.Context, tx pgx.Tx, item inventory.Item, now time.Time) error {
	_, err := tx.Exec(ctx, `WITH candidates AS (
		SELECT c.id,i.id AS inventory_id,CASE WHEN c.year IS NOT NULL AND i.year IS NOT NULL THEN 'title_year' ELSE 'title_only' END AS confidence
		FROM inventory_items i JOIN catalog_items c ON `+candidateMatch+`
		WHERE i.source_id=$1 AND i.source_item_id=$2
	), inserted AS (
		INSERT INTO match_suggestions(id,catalog_item_id,inventory_item_id,confidence,reason,created_at,last_seen_at)
		SELECT gen_random_uuid(),c.id,c.inventory_id,c.confidence,
		CASE WHEN c.confidence='title_year' THEN 'Normalized title and year match; no stable identifier match.'
		ELSE 'Normalized title matches; year is missing on one or both items; no stable identifier match.' END,$3,$3
		FROM candidates c WHERE true
		ON CONFLICT(catalog_item_id,inventory_item_id) DO UPDATE SET
		confidence=EXCLUDED.confidence,reason=EXCLUDED.reason,last_seen_at=EXCLUDED.last_seen_at RETURNING id
	) INSERT INTO match_suggestion_events(id,suggestion_id,event_type,created_at)
	SELECT gen_random_uuid(),id,'match.suggested',$3 FROM inserted ON CONFLICT(suggestion_id,event_type) DO NOTHING`, item.SourceID, item.SourceItemID, now)
	if err != nil {
		return fmt.Errorf("record match suggestions: %w", err)
	}
	return nil
}

// Accepted matches are local to this library item, never global provider
// aliases. Only candidates revalidated in this snapshot can supply evidence.
func acceptedMatches(ctx context.Context, tx pgx.Tx, item inventory.Item, now time.Time) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT m.catalog_item_id::text FROM match_suggestions m
		JOIN inventory_items i ON i.id=m.inventory_item_id
		WHERE i.source_id=$1 AND i.source_item_id=$2 AND m.status='accepted' AND m.last_seen_at=$3
		ORDER BY m.catalog_item_id`, item.SourceID, item.SourceItemID, now)
	if err != nil {
		return nil, fmt.Errorf("load accepted matches: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan accepted match: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// candidateMatch is shared by discovery and review, both against persisted
// inventory rows so unknown years consistently use NULL, not an adapter's zero.
const candidateMatch = `c.media_type=i.media_type AND requestarr_match_title(c.title)<>''
	AND requestarr_match_title(c.title)=requestarr_match_title(i.title)
	AND (c.year IS NULL OR i.year IS NULL OR c.year=i.year)`

// currentMatch restricts reviews to a candidate seen in the latest successful
// snapshot and still matching the current catalog metadata. It also hides old
// suggestions superseded by exact identifier matches on a later sync.
const currentMatch = `s.enabled AND s.authoritative_media_types ? c.media_type
	AND i.last_seen_at=s.last_sync_succeeded_at AND m.last_seen_at=i.last_seen_at
	AND ` + candidateMatch

// ListMatchSuggestions returns at most 100 pending candidates still present in
// the latest complete sync. Failed syncs leave that last-known view unchanged.
func (s *Store) ListMatchSuggestions(ctx context.Context) ([]domain.MatchSuggestion, error) {
	rows, err := s.pool.Query(ctx, `SELECT m.id::text,c.id::text,c.title,COALESCE(c.year,0),i.title,COALESCE(i.year,0),c.media_type,COALESCE(i.platform_key,''),s.name,i.source_item_id,m.confidence,m.reason,i.last_seen_at
		FROM match_suggestions m JOIN catalog_items c ON c.id=m.catalog_item_id
		JOIN inventory_items i ON i.id=m.inventory_item_id JOIN library_sources s ON s.id=i.source_id
		WHERE m.status='pending' AND `+currentMatch+`
		ORDER BY m.created_at,m.id LIMIT 100`)
	if err != nil {
		return nil, fmt.Errorf("list match suggestions: %w", err)
	}
	defer rows.Close()
	result := make([]domain.MatchSuggestion, 0)
	for rows.Next() {
		var m domain.MatchSuggestion
		if err := rows.Scan(&m.ID, &m.CatalogItemID, &m.CatalogTitle, &m.CatalogYear, &m.InventoryTitle, &m.InventoryYear, &m.MediaType, &m.PlatformKey, &m.SourceName, &m.SourceItemID, &m.Confidence, &m.Reason, &m.ObservedAt); err != nil {
			return nil, fmt.Errorf("scan match suggestion: %w", err)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// ReviewMatchSuggestion serializes with snapshot persistence. Retrying the
// same decision is a no-op; an opposing decision cannot reverse a final review.
// HTTP authorization is enforced by the caller, which supplies the actor ID.
func (s *Store) ReviewMatchSuggestion(ctx context.Context, id, actorID, decision string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin match review: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, persistLockKey); err != nil {
		return fmt.Errorf("lock match review: %w", err)
	}
	var prior, catalogID, sourceItemID, platformKey string
	var d inventory.Descriptor
	var observed time.Time
	var current bool
	err = tx.QueryRow(ctx, `SELECT m.status,c.id::text,s.id::text,s.kind,i.source_item_id,COALESCE(i.platform_key,''),i.last_seen_at,(`+currentMatch+`)
		FROM match_suggestions m JOIN catalog_items c ON c.id=m.catalog_item_id
		JOIN inventory_items i ON i.id=m.inventory_item_id JOIN library_sources s ON s.id=i.source_id
		WHERE m.id=$1 FOR UPDATE OF m,c,i,s`, id).Scan(&prior, &catalogID, &d.ID, &d.Kind, &sourceItemID, &platformKey, &observed, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrMatchNotFound
	}
	if err != nil {
		return fmt.Errorf("load match review: %w", err)
	}
	status, err := domain.MatchReviewTransition(prior, decision, current)
	if err != nil {
		return err
	}
	if prior == status {
		return nil
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE match_suggestions SET status=$2,reviewed_by=$3,reviewed_at=$4 WHERE id=$1`, id, status, actorID, now); err != nil {
		return fmt.Errorf("record match review: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO match_suggestion_events(id,suggestion_id,event_type,actor_user_id,created_at) VALUES($1,$2,$3,$4,$5)`, newUUID(), id, "match."+status, actorID, now); err != nil {
		return fmt.Errorf("audit match review: %w", err)
	}
	if decision == "accept" {
		if err := recordPresence(ctx, tx, d, catalogID, sourceItemID, platformKey, observed); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit match review: %w", err)
	}
	return nil
}
