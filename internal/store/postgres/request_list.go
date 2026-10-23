package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const maxRequestSnapshotItems = 10000

// List freezes membership/order once and loads live details for each page.
// No live OFFSET or mutable status/title keyset can skip or duplicate members.
func (s *Store) List(ctx context.Context, userID string, query requests.ListQuery) (requests.Page, error) {
	query, err := query.Normalize()
	if err != nil {
		return requests.Page{}, err
	}
	queryJSON, err := json.Marshal(query)
	if err != nil {
		return requests.Page{}, fmt.Errorf("encode request-list query: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return requests.Page{}, fmt.Errorf("begin request list: %w", err)
	}
	defer tx.Rollback(ctx)
	var ids []string
	var key []byte
	cursor := requestCursor{Version: 1}
	total := 0
	if query.Cursor == "" {
		ids, err = matchingRequestIDs(ctx, tx, userID, query)
		if err != nil {
			return requests.Page{}, err
		}
		total = len(ids)
		if total > maxRequestSnapshotItems {
			return requests.Page{}, requests.ErrQueryTooLarge
		}
		if total > query.Limit {
			cursor.Snapshot, key, err = createRequestSnapshot(ctx, tx, userID, string(queryJSON), ids)
			if err != nil {
				return requests.Page{}, err
			}
			ids = ids[:query.Limit]
		}
	} else {
		var payload, signature []byte
		cursor, payload, signature, err = decodeRequestCursor(query.Cursor)
		if err != nil {
			return requests.Page{}, err
		}
		var owner, queryKey string
		var current bool
		err = tx.QueryRow(ctx, `SELECT user_id::text,query_key,cursor_key,request_ids[$2:$3]::text[],cardinality(request_ids),expires_at>clock_timestamp()
			FROM request_list_snapshots WHERE id=$1 FOR SHARE`, cursor.Snapshot, cursor.Position+1, cursor.Position+query.Limit).Scan(&owner, &queryKey, &key, &ids, &total, &current)
		if errors.Is(err, pgx.ErrNoRows) {
			return requests.Page{}, requests.ErrCursorExpired
		}
		if err != nil {
			return requests.Page{}, fmt.Errorf("load request-list snapshot: %w", err)
		}
		if owner != userID || queryKey != string(queryJSON) || !validRequestCursorMAC(payload, signature, key) || cursor.Position >= total || cursor.Position%query.Limit != 0 {
			return requests.Page{}, requests.ErrInvalidCursor
		}
		if !current {
			return requests.Page{}, requests.ErrCursorExpired
		}
	}
	items, err := loadRequestPage(ctx, tx, userID, ids)
	if err != nil {
		return requests.Page{}, err
	}
	page := requests.Page{Items: items}
	cursor.Position += len(ids)
	if cursor.Position < total {
		next := encodeRequestCursor(cursor, key)
		page.NextCursor = &next
	}
	if err := tx.Commit(ctx); err != nil {
		return requests.Page{}, fmt.Errorf("commit request list: %w", err)
	}
	return page, nil
}

func matchingRequestIDs(ctx context.Context, tx pgx.Tx, userID string, query requests.ListQuery) ([]string, error) {
	order := "r.requested_at DESC,r.id"
	switch query.Sort {
	case "oldest":
		order = "r.requested_at,r.id"
	case "status":
		order = "CASE r.status WHEN 'requested' THEN 0 WHEN 'approved' THEN 1 WHEN 'available' THEN 2 WHEN 'denied' THEN 3 ELSE 4 END,r.requested_at DESC,r.id"
	}
	// Only fixed ORDER BY fragments are interpolated; all user values are bound.
	rows, err := tx.Query(ctx, `SELECT r.id::text FROM requests r JOIN catalog_items c ON c.id=r.catalog_item_id
		WHERE ($2='' OR strpos(lower(c.title),lower($2))>0) AND ($3='' OR r.status=$3)
		AND ($4='' OR c.media_type=$4) AND (NOT $5 OR EXISTS (SELECT 1 FROM request_subscriptions s WHERE s.request_id=r.id AND s.user_id=$1))
		ORDER BY `+order+` LIMIT $6`, userID, query.Q, query.Status, query.MediaType, query.Mine, maxRequestSnapshotItems+1)
	if err != nil {
		return nil, fmt.Errorf("match request-list IDs: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan request-list ID: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func createRequestSnapshot(ctx context.Context, tx pgx.Tx, userID, queryKey string, ids []string) (string, []byte, error) {
	// Serialize retention for a user's concurrent first-page requests. Cleanup
	// work is bounded, and SKIP LOCKED keeps unrelated users from blocking it.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "requestarr:request-list:"+userID); err != nil {
		return "", nil, fmt.Errorf("lock request-list retention: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM request_list_snapshots WHERE id IN
		(SELECT id FROM request_list_snapshots WHERE expires_at<=clock_timestamp() ORDER BY expires_at,id LIMIT 100 FOR UPDATE SKIP LOCKED)`); err != nil {
		return "", nil, fmt.Errorf("clean expired request-list snapshots: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM request_list_snapshots WHERE id IN
		(SELECT id FROM request_list_snapshots WHERE user_id=$1 ORDER BY created_at DESC,id DESC OFFSET 4)`, userID); err != nil {
		return "", nil, fmt.Errorf("prune request-list snapshots: %w", err)
	}
	id := newUUID()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", nil, fmt.Errorf("generate request-list signing key: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO request_list_snapshots(id,user_id,query_key,cursor_key,request_ids) VALUES($1,$2,$3,$4,$5::uuid[])`, id, userID, queryKey, key, ids); err != nil {
		return "", nil, fmt.Errorf("persist request-list snapshot: %w", err)
	}
	return id, key, nil
}

func loadRequestPage(ctx context.Context, tx pgx.Tx, userID string, ids []string) ([]domain.Request, error) {
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.catalog_item_id::text,c.title,c.media_type,COALESCE(c.artwork_url,''),c.artwork_kind,r.status,r.version,r.requested_by::text,COALESCE(r.requester_note,''),r.acceptable_platforms,r.requested_at,r.updated_at,r.available_at,c.availability,(SELECT MAX(e.observed_at) FROM availability_evidence e WHERE e.catalog_item_id=c.id),
		COALESCE((SELECT source FROM request_subscriptions s WHERE s.request_id=r.id AND s.user_id=$1),''),
		COALESCE((SELECT array_agg(p.name ORDER BY p.name) FROM catalog_item_platforms p WHERE p.catalog_item_id=r.catalog_item_id AND (r.acceptable_platforms->>'any'='true' OR p.platform_key IN (SELECT jsonb_array_elements_text(r.acceptable_platforms->'platformKeys')))), ARRAY[]::text[])
		FROM unnest($2::uuid[]) WITH ORDINALITY AS p(id,position)
		JOIN requests r ON r.id=p.id JOIN catalog_items c ON c.id=r.catalog_item_id ORDER BY p.position`, userID, ids)
	if err != nil {
		return nil, fmt.Errorf("load request page: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Request, 0, len(ids))
	for rows.Next() {
		var r domain.Request
		var platformNames pgtype.FlatArray[string]
		if err := rows.Scan(&r.ID, &r.CatalogItemID, &r.Title, &r.MediaType, &r.ArtworkURL, &r.ArtworkKind, &r.Status, &r.Version, &r.RequestedBy, &r.Note, requestJSON(&r), &r.CreatedAt, &r.UpdatedAt, &r.AvailableAt, &r.Availability, &r.AvailabilityObservedAt, &r.SubscriptionSource, &platformNames); err != nil {
			return nil, fmt.Errorf("scan request page: %w", err)
		}
		r.PlatformNames = []string(platformNames)
		r.Subscribed = r.SubscriptionSource != ""
		items = append(items, r)
	}
	return items, rows.Err()
}
