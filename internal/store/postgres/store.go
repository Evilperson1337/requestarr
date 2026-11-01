// Package postgres is the first concrete persistence adapter: it owns the
// connection pool, applies embedded migrations at startup, and answers
// readiness checks. Domain-specific repositories belong here in later phases;
// this package intentionally does not know about requests, users, or catalog
// items yet.
package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

var _ requests.Repository = (*Store)(nil)

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (s *Store) Upsert(ctx context.Context, resolved domain.ResolvedItem) (domain.CatalogItem, error) {
	i := resolved.Item
	if i.MediaType == "" || i.Provider == "" || i.ProviderID == "" || i.Title == "" || len(i.Title) > 512 || len(i.ProviderID) > 128 || len(i.ArtworkURL) > 2048 || len(i.ExternalURL) > 2048 {
		return domain.CatalogItem{}, fmt.Errorf("invalid catalog item")
	}
	if (i.MediaType != domain.MediaMovie && i.MediaType != domain.MediaTV && i.MediaType != domain.MediaAlbum && i.MediaType != domain.MediaSong && i.MediaType != domain.MediaGame) ||
		(i.Provider != "tmdb" && i.Provider != "musicbrainz" && i.Provider != "igdb") {
		return domain.CatalogItem{}, fmt.Errorf("invalid catalog item type or provider")
	}
	if len(resolved.Platforms) > 250 {
		return domain.CatalogItem{}, fmt.Errorf("too many catalog platforms")
	}
	for _, p := range resolved.Platforms {
		if p.Key == "" || p.Name == "" || len(p.Key) > 128 || len(p.Name) > 256 || len(p.ProviderID) > 128 {
			return domain.CatalogItem{}, fmt.Errorf("invalid catalog platform")
		}
	}
	if i.ArtworkKind == "" {
		if i.MediaType == domain.MediaAlbum || i.MediaType == domain.MediaSong {
			i.ArtworkKind = domain.ArtworkSquare
		} else {
			i.ArtworkKind = domain.ArtworkPoster
		}
	}
	metadata, err := json.Marshal(i.Metadata)
	if err != nil {
		return domain.CatalogItem{}, fmt.Errorf("encode catalog metadata: %w", err)
	}
	if i.ID == "" {
		i.ID = newUUID()
		if i.ID == "" {
			return domain.CatalogItem{}, fmt.Errorf("generate catalog item ID")
		}
	}
	if i.UpdatedAt.IsZero() {
		i.UpdatedAt = time.Now().UTC()
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,year,artwork_url,artwork_kind,external_url,metadata,metadata_fetched_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,0),$7,$8,$9,$10,now(),now(),$11) ON CONFLICT(provider,provider_id,media_type) DO UPDATE SET title=EXCLUDED.title,year=EXCLUDED.year,artwork_url=EXCLUDED.artwork_url,artwork_kind=EXCLUDED.artwork_kind,external_url=EXCLUDED.external_url,metadata=EXCLUDED.metadata,metadata_fetched_at=now(),updated_at=EXCLUDED.updated_at RETURNING id::text,updated_at`, i.ID, i.MediaType, i.Provider, i.ProviderID, i.Title, i.Year, i.ArtworkURL, i.ArtworkKind, i.ExternalURL, metadata, i.UpdatedAt).Scan(&i.ID, &i.UpdatedAt)
	if err != nil {
		return domain.CatalogItem{}, fmt.Errorf("upsert catalog item: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.CatalogItem{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM catalog_item_platforms WHERE catalog_item_id=$1`, i.ID); err != nil {
		return domain.CatalogItem{}, fmt.Errorf("replace catalog platforms: %w", err)
	}
	for _, p := range resolved.Platforms {
		if _, err := tx.Exec(ctx, `INSERT INTO catalog_item_platforms(catalog_item_id,provider_id,platform_key,name) VALUES($1,$2,$3,$4)`, i.ID, p.ProviderID, p.Key, p.Name); err != nil {
			return domain.CatalogItem{}, fmt.Errorf("save catalog platform: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.CatalogItem{}, fmt.Errorf("commit catalog platforms: %w", err)
	}
	return i, nil
}

func (s *Store) CreateOrGet(ctx context.Context, request domain.Request, sub domain.RequestSubscription) (domain.Request, bool, error) {
	if request.CatalogItemID == "" || request.RequestedBy == "" {
		return domain.Request{}, false, fmt.Errorf("invalid request")
	}
	if request.Status != domain.RequestRequested || sub.UserID != request.RequestedBy || sub.Source != domain.SubscriptionRequester {
		return domain.Request{}, false, fmt.Errorf("invalid request creation state")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Request{}, false, err
	}
	defer tx.Rollback(ctx)
	var out domain.Request
	scanner := requestJSON(&out)
	err = tx.QueryRow(ctx, `INSERT INTO requests(id,catalog_item_id,status,requested_by,requester_note,acceptable_platforms,selection,requested_at,updated_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,now(),now()) ON CONFLICT(catalog_item_id) DO NOTHING RETURNING id::text,catalog_item_id::text,status,requested_by::text,COALESCE(requester_note,''),acceptable_platforms,requested_at,updated_at,available_at`, newUUID(), request.CatalogItemID, request.Status, request.RequestedBy, request.Note, mustJSON(request.Platforms), mustJSON(request.Selection)).Scan(&out.ID, &out.CatalogItemID, &out.Status, &out.RequestedBy, &out.Note, scanner, &out.CreatedAt, &out.UpdatedAt, &out.AvailableAt)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO request_subscriptions(request_id,user_id,source,created_at) VALUES($1,$2,$3,now())`, out.ID, sub.UserID, sub.Source)
		if err != nil {
			return domain.Request{}, false, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO request_events(id,request_id,event_type,actor_user_id,data,created_at) VALUES($1,$2,'request.created',$3,'{}',now())`, newUUID(), out.ID, request.RequestedBy)
		if err != nil {
			return domain.Request{}, false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.Request{}, false, err
		}
		out.Selection = request.Selection
		return out, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Request{}, false, err
	}
	var selection []byte
	err = tx.QueryRow(ctx, `SELECT id::text,catalog_item_id::text,status,requested_by::text,COALESCE(requester_note,''),acceptable_platforms,requested_at,updated_at,available_at,selection FROM requests WHERE catalog_item_id=$1`, request.CatalogItemID).Scan(&out.ID, &out.CatalogItemID, &out.Status, &out.RequestedBy, &out.Note, scanner, &out.CreatedAt, &out.UpdatedAt, &out.AvailableAt, &selection)
	if err != nil {
		return domain.Request{}, false, err
	}
	if out.Selection, err = parseSelection(selection); err != nil {
		return domain.Request{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Request{}, false, err
	}
	return out, false, nil
}

func parseSelection(raw []byte) (domain.Selection, error) {
	var selection domain.Selection
	if len(raw) == 0 {
		return selection, nil
	}
	if err := json.Unmarshal(raw, &selection); err != nil {
		return domain.Selection{}, fmt.Errorf("invalid request selection: %w", err)
	}
	return selection, nil
}

// MergeSelection unions selection into an open Request under a row lock, follows
// the Request for the user and bumps its version when the selection grew.
func (s *Store) MergeSelection(ctx context.Context, requestID, userID string, selection domain.Selection) (domain.Request, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Request{}, false, err
	}
	defer tx.Rollback(ctx)
	var out domain.Request
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT id::text,catalog_item_id::text,status,requested_by::text,COALESCE(requester_note,''),acceptable_platforms,requested_at,updated_at,available_at,selection FROM requests WHERE id=$1 FOR UPDATE`, requestID).Scan(&out.ID, &out.CatalogItemID, &out.Status, &out.RequestedBy, &out.Note, requestJSON(&out), &out.CreatedAt, &out.UpdatedAt, &out.AvailableAt, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Request{}, false, requests.ErrRequestNotFound
	}
	if err != nil {
		return domain.Request{}, false, err
	}
	if out.Selection, err = parseSelection(raw); err != nil {
		return domain.Request{}, false, err
	}
	if !out.Status.IsOpen() || out.Selection.Contains(selection) {
		return out, false, nil
	}
	merged := out.Selection.Merge(selection)
	if _, err = tx.Exec(ctx, `UPDATE requests SET selection=$2,updated_at=now(),version=version+1 WHERE id=$1`, requestID, mustJSON(merged)); err != nil {
		return domain.Request{}, false, err
	}
	if err = appendRequestEvent(ctx, tx, requestID, userID, "request.selection_changed", mustJSON(merged)); err != nil {
		return domain.Request{}, false, err
	}
	ct, err := tx.Exec(ctx, `INSERT INTO request_subscriptions(request_id,user_id,source,created_at) VALUES($1,$2,'manual',now()) ON CONFLICT(request_id,user_id) DO NOTHING`, requestID, userID)
	if err != nil {
		return domain.Request{}, false, err
	}
	if ct.RowsAffected() == 1 {
		if err = appendSubscriptionEvent(ctx, tx, requestID, userID, "request.subscribed"); err != nil {
			return domain.Request{}, false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Request{}, false, err
	}
	out.Selection = merged
	return out, true, nil
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

type jsonScanner struct{ target *domain.Request }

func requestJSON(r *domain.Request) any { return &jsonScanner{target: r} }
func (j *jsonScanner) Scan(src any) error {
	b, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("invalid request JSON")
	}
	return json.Unmarshal(b, &j.target.Platforms)
}

func (s *Store) Subscribe(ctx context.Context, requestID, userID string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM requests WHERE id=$1 FOR UPDATE`, requestID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return false, requests.ErrRequestNotFound
	} else if err != nil {
		return false, err
	}
	if !domain.RequestStatus(status).IsOpen() {
		return false, requests.ErrRequestClosed
	}
	ct, err := tx.Exec(ctx, `INSERT INTO request_subscriptions(request_id,user_id,source,created_at) VALUES($1,$2,'manual',now()) ON CONFLICT(request_id,user_id) DO NOTHING`, requestID, userID)
	if err != nil {
		return false, err
	}
	changed := ct.RowsAffected() == 1
	if changed {
		if err = appendSubscriptionEvent(ctx, tx, requestID, userID, "request.subscribed"); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return changed, nil
}

// Unsubscribe removes the user's subscription (D016). When that was the last
// subscriber of an open Request, the Request becomes cancelled in the same
// transaction with a version bump and an audit event, but only when
// confirmCancel is set; otherwise nothing changes and the caller must confirm.
func (s *Store) Unsubscribe(ctx context.Context, requestID, userID string, confirmCancel bool) (changed, cancelled bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM requests WHERE id=$1 FOR UPDATE`, requestID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return false, false, requests.ErrRequestNotFound
	} else if err != nil {
		return false, false, err
	}
	var subscribed bool
	var remaining int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(bool_or(user_id=$2),false),count(*) FROM request_subscriptions WHERE request_id=$1`, requestID, userID).Scan(&subscribed, &remaining); err != nil {
		return false, false, err
	}
	if !subscribed {
		return false, false, tx.Commit(ctx)
	}
	willCancel := remaining == 1 && domain.RequestStatus(status).IsOpen()
	if willCancel && !confirmCancel {
		return false, false, requests.ErrCancelNeedsConfirmation
	}
	if _, err = tx.Exec(ctx, `DELETE FROM request_subscriptions WHERE request_id=$1 AND user_id=$2`, requestID, userID); err != nil {
		return false, false, err
	}
	if err = appendSubscriptionEvent(ctx, tx, requestID, userID, "request.unsubscribed"); err != nil {
		return false, false, err
	}
	if willCancel {
		var version int64
		if err = tx.QueryRow(ctx, `UPDATE requests SET status='cancelled',updated_at=now(),version=version+1 WHERE id=$1 RETURNING version`, requestID).Scan(&version); err != nil {
			return false, false, err
		}
		data := mustJSON(map[string]any{"from": status, "to": domain.RequestCancelled, "version": version, "reason": "last_subscriber_left"})
		if err = appendRequestEvent(ctx, tx, requestID, userID, "request.cancelled", data); err != nil {
			return false, false, err
		}
		cancelled = true
	}
	if err = tx.Commit(ctx); err != nil {
		return false, false, err
	}
	return true, cancelled, nil
}

func appendSubscriptionEvent(ctx context.Context, tx pgx.Tx, requestID, userID, eventType string) error {
	return appendRequestEvent(ctx, tx, requestID, userID, eventType, []byte("{}"))
}

func appendRequestEvent(ctx context.Context, tx pgx.Tx, requestID, userID, eventType string, data []byte) error {
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(aggregate_sequence),0)+1 FROM request_events WHERE request_id=$1`, requestID).Scan(&sequence); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO request_events(id,request_id,event_type,actor_user_id,data,created_at,aggregate_sequence) VALUES($1,$2,$3,$4,$5,now(),$6)`, newUUID(), requestID, eventType, userID, data, sequence)
	return err
}

// Transition locks the request row so expected-version checks and event
// sequence allocation are serialized with every lifecycle change.
func (s *Store) Transition(ctx context.Context, in requests.TransitionInput) (domain.Request, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Request{}, false, err
	}
	defer tx.Rollback(ctx)
	var out domain.Request
	var currentVersion int64
	var availableAt pgtype.Timestamptz
	err = tx.QueryRow(ctx, `SELECT id::text,catalog_item_id::text,status,requested_by::text,COALESCE(requester_note,''),acceptable_platforms,requested_at,updated_at,available_at,version FROM requests WHERE id=$1 FOR UPDATE`, in.RequestID).Scan(&out.ID, &out.CatalogItemID, &out.Status, &out.RequestedBy, &out.Note, requestJSON(&out), &out.CreatedAt, &out.UpdatedAt, &availableAt, &currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Request{}, false, requests.ErrRequestNotFound
	}
	if err != nil {
		return domain.Request{}, false, err
	}
	if currentVersion != in.ExpectedVersion {
		return domain.Request{}, false, requests.ErrTransitionConflict
	}
	if !domain.CanTransition(out.Status, in.ToStatus) {
		return domain.Request{}, false, requests.ErrTransitionInvalid
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(aggregate_sequence),0)+1 FROM request_events WHERE request_id=$1`, in.RequestID).Scan(&sequence); err != nil {
		return domain.Request{}, false, err
	}
	data := mustJSON(map[string]any{"from": out.Status, "to": in.ToStatus, "version": in.ExpectedVersion + 1, "operationId": in.OperationID})
	if _, err := tx.Exec(ctx, `UPDATE requests SET status=$2,reviewed_by=$3,reviewed_at=now(),updated_at=now(),version=version+1 WHERE id=$1 AND version=$4`, in.RequestID, in.ToStatus, in.Actor.ID, in.ExpectedVersion); err != nil {
		return domain.Request{}, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO request_events(id,request_id,event_type,actor_user_id,data,created_at,aggregate_sequence,operation_id) VALUES($1,$2,$3,$4,$5,now(),$6,$7)`, newUUID(), in.RequestID, "request."+string(in.ToStatus), in.Actor.ID, data, sequence, in.OperationID); err != nil {
		return domain.Request{}, false, err
	}
	var title string
	if err := tx.QueryRow(ctx, `SELECT title FROM catalog_items WHERE id=$1`, out.CatalogItemID).Scan(&title); err != nil {
		return domain.Request{}, false, err
	}
	if err := enqueueRequestNotifications(ctx, tx, in.RequestID, "request."+string(in.ToStatus), title, in.ToStatus); err != nil {
		return domain.Request{}, false, err
	}
	if err := tx.QueryRow(ctx, `SELECT updated_at FROM requests WHERE id=$1`, in.RequestID).Scan(&out.UpdatedAt); err != nil {
		return domain.Request{}, false, err
	}
	out.Status, out.Version = in.ToStatus, currentVersion+1
	if availableAt.Valid {
		out.AvailableAt = &availableAt.Time
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Request{}, false, err
	}
	return out, true, nil
}

func enqueueRequestNotifications(ctx context.Context, tx pgx.Tx, requestID, eventKey, title string, status domain.RequestStatus) error {
	body := "Your request was " + string(status) + "."
	if _, err := tx.Exec(ctx, `INSERT INTO notification_deliveries(id,event_key,request_event_id,user_id,channel,status,title,body,created_at,available_at,updated_at)
		SELECT gen_random_uuid(),$2,e.id,s.user_id,'in_app','pending',$3,$4,now(),now(),now()
		FROM request_events e JOIN request_subscriptions s ON s.request_id=e.request_id
		WHERE e.request_id=$1 AND e.event_type=$2
		ON CONFLICT(event_key,user_id,channel) WHERE event_key IS NOT NULL DO NOTHING`, requestID, eventKey, title, body); err != nil {
		return fmt.Errorf("enqueue request notifications: %w", err)
	}
	return enqueueGlobalAppriseEvent(ctx, tx, eventKey+":"+requestID, title, body)
}

func (s *Store) CatalogPlatforms(ctx context.Context, catalogID string) ([]domain.Platform, error) {
	rows, err := s.pool.Query(ctx, `SELECT provider_id,platform_key,name FROM catalog_item_platforms WHERE catalog_item_id=$1 ORDER BY name,platform_key`, catalogID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var platforms []domain.Platform
	for rows.Next() {
		var p domain.Platform
		if err := rows.Scan(&p.ProviderID, &p.Key, &p.Name); err != nil {
			return nil, err
		}
		platforms = append(platforms, p)
	}
	return platforms, rows.Err()
}

func (s *Store) ValidatePlatforms(ctx context.Context, catalogID string, selection domain.PlatformSelection) error {
	var mediaType domain.MediaType
	if err := s.pool.QueryRow(ctx, `SELECT media_type FROM catalog_items WHERE id=$1`, catalogID).Scan(&mediaType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return requests.ErrCatalogNotFound
		}
		return err
	}
	if mediaType != domain.MediaGame {
		return nil
	}
	if selection.Any {
		return nil
	}
	var matched int
	if err := s.pool.QueryRow(ctx, `SELECT count(DISTINCT platform_key) FROM catalog_item_platforms WHERE catalog_item_id=$1 AND platform_key=ANY($2)`, catalogID, selection.PlatformKeys).Scan(&matched); err != nil {
		return err
	}
	if matched != len(selection.PlatformKeys) {
		return fmt.Errorf("one or more platform keys are not in the IGDB catalog")
	}
	return nil
}

func (s *Store) CatalogMediaType(ctx context.Context, catalogID string) (domain.MediaType, error) {
	var mediaType domain.MediaType
	err := s.pool.QueryRow(ctx, `SELECT media_type FROM catalog_items WHERE id=$1`, catalogID).Scan(&mediaType)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", requests.ErrCatalogNotFound
	}
	return mediaType, err
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
