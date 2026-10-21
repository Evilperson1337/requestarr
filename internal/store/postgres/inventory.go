package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/example/requestarr/internal/domain"
	"github.com/example/requestarr/internal/inventory"
	"github.com/jackc/pgx/v5"
)

const jellyfinSourceID = "5efcc577-5236-4ce3-b2c0-b9d58c64a718"

// PersistJellyfinSnapshot atomically stores a complete Jellyfin snapshot and
// reconciles exact TMDB availability evidence. Incomplete runs never reach here.
func (s *Store) PersistJellyfinSnapshot(ctx context.Context, baseURL string, snap inventory.Snapshot) error {
	if !snap.Complete || snap.SourceID != jellyfinSourceID {
		return fmt.Errorf("refusing incomplete or unexpected Jellyfin snapshot")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Jellyfin inventory transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	now := snap.ObservedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if _, err = tx.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,enabled,authoritative_media_types,created_at,updated_at) VALUES($1,'jellyfin','Jellyfin',$2,true,'["movie","tv"]'::jsonb,now(),now()) ON CONFLICT(id) DO UPDATE SET base_url=EXCLUDED.base_url,enabled=true,updated_at=now()`, jellyfinSourceID, baseURL); err != nil {
		return fmt.Errorf("upsert Jellyfin source: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE library_sources SET last_sync_started_at=$2,last_sync_succeeded_at=$2,last_sync_error=NULL,updated_at=now() WHERE id=$1`, jellyfinSourceID, now); err != nil {
		return fmt.Errorf("update Jellyfin sync health: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO jellyfin_reconciliation_settings(source_id) VALUES($1) ON CONFLICT(source_id) DO NOTHING`, jellyfinSourceID); err != nil {
		return fmt.Errorf("ensure Jellyfin reconciliation settings: %w", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM jellyfin_absence_state WHERE catalog_item_id IN (SELECT catalog_item_id FROM availability_evidence WHERE source_id=$1) AND EXISTS (SELECT 1 FROM availability_evidence e JOIN library_sources s ON s.id=e.source_id WHERE e.catalog_item_id=jellyfin_absence_state.catalog_item_id AND e.source_id<>$1 AND s.enabled AND s.authoritative_media_types ? (SELECT media_type FROM catalog_items WHERE id=jellyfin_absence_state.catalog_item_id))`, jellyfinSourceID); err != nil {
		return fmt.Errorf("clear absence for alternate positive evidence: %w", err)
	}
	var allowEmpty bool
	if err = tx.QueryRow(ctx, `SELECT allow_empty_snapshots FROM jellyfin_reconciliation_settings WHERE source_id=$1 FOR UPDATE`, jellyfinSourceID).Scan(&allowEmpty); err != nil {
		return fmt.Errorf("read Jellyfin reconciliation settings: %w", err)
	}
	if len(snap.Items) > 0 || allowEmpty {
		if _, err = tx.Exec(ctx, `INSERT INTO jellyfin_snapshot_runs(observed_at,item_count) VALUES($1,$2)`, now, len(snap.Items)); err != nil {
			return fmt.Errorf("record Jellyfin snapshot run: %w", err)
		}
	}
	seen := make(map[string]struct{}, len(snap.Items))
	for _, item := range snap.Items {
		if strings.TrimSpace(item.SourceID) != jellyfinSourceID || strings.TrimSpace(item.SourceItemID) == "" || (item.MediaType != "movie" && item.MediaType != "tv") {
			return fmt.Errorf("invalid Jellyfin inventory item")
		}
		ids, err := json.Marshal(item.ExternalIDs)
		if err != nil {
			return fmt.Errorf("encode Jellyfin provider IDs: %w", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO inventory_items(id,source_id,source_item_id,media_type,title,year,external_ids,first_seen_at,last_seen_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,0),$7,$8,$8) ON CONFLICT(source_id,source_item_id) DO UPDATE SET media_type=EXCLUDED.media_type,title=EXCLUDED.title,year=EXCLUDED.year,external_ids=EXCLUDED.external_ids,last_seen_at=EXCLUDED.last_seen_at`, newUUID(), jellyfinSourceID, item.SourceItemID, item.MediaType, item.Title, item.Year, ids, now); err != nil {
			return fmt.Errorf("upsert Jellyfin inventory item: %w", err)
		}
		tmdb := strings.TrimSpace(item.ExternalIDs["tmdb"])
		if tmdb == "" {
			continue
		}
		var catalogID string
		err = tx.QueryRow(ctx, `SELECT id::text FROM catalog_items WHERE provider='tmdb' AND media_type=$1 AND provider_id=$2`, item.MediaType, tmdb).Scan(&catalogID)
		if err == pgx.ErrNoRows {
			continue
		}
		if err != nil {
			return fmt.Errorf("match Jellyfin inventory item: %w", err)
		}
		seen[catalogID] = struct{}{}
		if _, err = tx.Exec(ctx, `DELETE FROM jellyfin_absence_state WHERE catalog_item_id=$1`, catalogID); err != nil {
			return fmt.Errorf("clear Jellyfin absence streak: %w", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO availability_evidence(catalog_item_id,source_id,source_item_id,observed_at) VALUES($1,$2,$3,$4) ON CONFLICT(catalog_item_id,source_id) DO UPDATE SET source_item_id=EXCLUDED.source_item_id,observed_at=EXCLUDED.observed_at`, catalogID, jellyfinSourceID, item.SourceItemID, now); err != nil {
			return fmt.Errorf("upsert availability evidence: %w", err)
		}
		var prior string
		var version int64
		if err = tx.QueryRow(ctx, `SELECT availability,availability_version FROM catalog_items WHERE id=$1 FOR UPDATE`, catalogID).Scan(&prior, &version); err != nil {
			return fmt.Errorf("read catalog availability: %w", err)
		}
		if prior != "available" {
			version++
			if _, err = tx.Exec(ctx, `UPDATE catalog_items SET availability='available',availability_version=$2,updated_at=$3 WHERE id=$1`, catalogID, version, now); err != nil {
				return fmt.Errorf("mark catalog item available: %w", err)
			}
			var insertedEventID string
			err = tx.QueryRow(ctx, `INSERT INTO availability_events(id,catalog_item_id,idempotency_key,event_type,data,created_at) VALUES($1,$2,$3,'availability.available','{}'::jsonb,$4) ON CONFLICT(idempotency_key) DO NOTHING RETURNING id::text`, newUUID(), catalogID, fmt.Sprintf("jellyfin:%s:availability:%d", catalogID, version), now).Scan(&insertedEventID)
			if err != nil && err != pgx.ErrNoRows {
				return fmt.Errorf("append availability event: %w", err)
			}
			if insertedEventID != "" {
				if err := enqueueAvailabilityNotifications(ctx, tx, catalogID, version); err != nil {
					return err
				}
			}
		}
	}
	if len(snap.Items) > 0 || allowEmpty {
		if err = reconcileJellyfinAbsences(ctx, tx, now, seen); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Jellyfin inventory snapshot: %w", err)
	}
	return nil
}

func reconcileJellyfinAbsences(ctx context.Context, tx pgx.Tx, now time.Time, seen map[string]struct{}) error {
	rows, err := tx.Query(ctx, `SELECT c.id::text,c.availability,c.availability_version,a.first_absent_at,a.last_absent_at,a.absence_count
		FROM catalog_items c JOIN availability_evidence e ON e.catalog_item_id=c.id AND e.source_id=$1
		JOIN inventory_items i ON i.source_id=e.source_id AND i.source_item_id=e.source_item_id
		LEFT JOIN jellyfin_absence_state a ON a.catalog_item_id=c.id
		WHERE c.media_type IN ('movie','tv') ORDER BY c.id FOR UPDATE OF c`, jellyfinSourceID)
	if err != nil {
		return fmt.Errorf("list Jellyfin availability candidates: %w", err)
	}
	type candidate struct {
		id, availability string
		version, count   int64
		first, last      *time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.availability, &c.version, &c.first, &c.last, &c.count); err != nil {
			rows.Close()
			return fmt.Errorf("scan Jellyfin availability candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate Jellyfin availability candidates: %w", err)
	}
	rows.Close()
	for _, c := range candidates {
		if _, ok := seen[c.id]; ok {
			continue
		}
		// Positive evidence from any other enabled authoritative source wins.
		var other bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM availability_evidence e JOIN library_sources s ON s.id=e.source_id WHERE e.catalog_item_id=$1 AND e.source_id<>$2 AND s.enabled AND s.authoritative_media_types ? (SELECT media_type FROM catalog_items WHERE id=$1))`, c.id, jellyfinSourceID).Scan(&other); err != nil {
			return fmt.Errorf("check other availability evidence: %w", err)
		}
		if other {
			if _, err := tx.Exec(ctx, `DELETE FROM jellyfin_absence_state WHERE catalog_item_id=$1`, c.id); err != nil {
				return fmt.Errorf("clear absence with alternate positive evidence: %w", err)
			}
			continue
		}
		if c.first == nil {
			if _, err := tx.Exec(ctx, `INSERT INTO jellyfin_absence_state(catalog_item_id,first_absent_at,last_absent_at,absence_count) VALUES($1,$2,$2,1) ON CONFLICT(catalog_item_id) DO NOTHING`, c.id, now); err != nil {
				return fmt.Errorf("record first Jellyfin absence: %w", err)
			}
			continue
		}
		if !now.After(*c.last) {
			continue
		}
		if now.Before(c.first.Add(24 * time.Hour)) {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE jellyfin_absence_state SET last_absent_at=$2,absence_count=absence_count+1 WHERE catalog_item_id=$1`, c.id, now); err != nil {
			return fmt.Errorf("advance Jellyfin absence streak: %w", err)
		}
		if c.count < 1 || c.availability != "available" {
			continue
		}
		version := c.version + 1
		if _, err := tx.Exec(ctx, `UPDATE catalog_items SET availability='missing',availability_version=$2,updated_at=$3 WHERE id=$1 AND availability='available'`, c.id, version, now); err != nil {
			return fmt.Errorf("mark catalog item missing: %w", err)
		}
		key := fmt.Sprintf("jellyfin:%s:availability:%d", c.id, version)
		if _, err := tx.Exec(ctx, `INSERT INTO availability_events(id,catalog_item_id,idempotency_key,event_type,data,created_at) VALUES($1,$2,$3,'availability.missing','{}'::jsonb,$4)`, newUUID(), c.id, key, now); err != nil {
			return fmt.Errorf("append missing availability event: %w", err)
		}
		if err := enqueueGlobalAppriseEvent(ctx, tx, key, "Availability changed", "A requested item is no longer available."); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM availability_evidence WHERE catalog_item_id=$1 AND source_id=$2`, c.id, jellyfinSourceID); err != nil {
			return fmt.Errorf("remove stale Jellyfin availability evidence: %w", err)
		}
	}
	return nil
}

func enqueueAvailabilityNotifications(ctx context.Context, tx pgx.Tx, catalogID string, version int64) error {
	key := fmt.Sprintf("availability:%s:%d", catalogID, version)
	query := `INSERT INTO notification_deliveries(id,event_key,user_id,channel,status,title,body,created_at,available_at,updated_at)
		SELECT gen_random_uuid(),$1,s.user_id,'in_app','pending',c.title,'A requested item is now available.',now(),now(),now()
		FROM requests r JOIN request_subscriptions s ON s.request_id=r.id JOIN catalog_items c ON c.id=r.catalog_item_id
		WHERE r.catalog_item_id=$2 AND c.availability='available'
		ON CONFLICT(event_key,user_id,channel) WHERE event_key IS NOT NULL DO NOTHING`
	if _, err := tx.Exec(ctx, query, key, catalogID); err != nil {
		return fmt.Errorf("enqueue availability inbox notifications: %w", err)
	}
	var title string
	if err := tx.QueryRow(ctx, `SELECT title FROM catalog_items WHERE id=$1`, catalogID).Scan(&title); err != nil {
		return err
	}
	return enqueueGlobalAppriseEvent(ctx, tx, key, title, "A requested item is now available.")
}

// RecordJellyfinFailure updates operational health without touching inventory or availability.
func (s *Store) RecordJellyfinFailure(ctx context.Context, started time.Time, safeError string) error {
	if safeError == "" {
		safeError = "sync failed"
	}
	if len(safeError) > 256 {
		safeError = safeError[:256]
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,enabled,authoritative_media_types,last_sync_started_at,last_sync_error,created_at,updated_at) VALUES($1,'jellyfin','Jellyfin','',true,'["movie","tv"]'::jsonb,$2,$3,now(),now()) ON CONFLICT(id) DO UPDATE SET last_sync_started_at=EXCLUDED.last_sync_started_at,last_sync_error=EXCLUDED.last_sync_error,updated_at=now()`, jellyfinSourceID, started, safeError)
	if err != nil {
		return fmt.Errorf("record Jellyfin sync failure: %w", err)
	}
	return nil
}

func (s *Store) JellyfinSyncStatus(ctx context.Context) (domain.SourceSyncStatus, error) {
	var out domain.SourceSyncStatus
	err := s.pool.QueryRow(ctx, `SELECT enabled,last_sync_started_at,last_sync_succeeded_at,last_sync_error FROM library_sources WHERE id=$1`, jellyfinSourceID).Scan(&out.Enabled, &out.LastSyncStartedAt, &out.LastSyncSucceededAt, &out.LastSyncError)
	if err != nil && err != pgx.ErrNoRows {
		return domain.SourceSyncStatus{}, fmt.Errorf("read Jellyfin sync status: %w", err)
	}
	out.Configured = err == nil
	if !out.Configured {
		out.Enabled = false
	}
	return out, nil
}
