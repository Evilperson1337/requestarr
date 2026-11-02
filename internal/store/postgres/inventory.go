package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// persistLockKey serializes snapshot persistence across Sources so two syncs
// touching the same catalog items cannot deadlock on their row locks.
const persistLockKey = "requestarr:persist-inventory-snapshot"

// absenceInterval is how far apart two complete syncs must be to count as
// separate absences of an item from one Source.
const absenceInterval = 24 * time.Hour

// PersistSnapshot atomically stores a complete snapshot from any Source,
// matches its identifiers to catalog items, and reconciles availability under
// the multi-Source rule (D015). Incomplete snapshots are refused: only a
// complete enumeration can add or remove evidence.
func (s *Store) PersistSnapshot(ctx context.Context, d inventory.Descriptor, baseURL string, snap inventory.Snapshot) error {
	if !snap.Complete || snap.SourceID != d.ID || d.ID == "" {
		return fmt.Errorf("refusing incomplete or unexpected %s snapshot", d.Kind)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin %s inventory transaction: %w", d.Kind, err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, persistLockKey); err != nil {
		return fmt.Errorf("lock inventory persistence: %w", err)
	}
	now := snap.ObservedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err = upsertSource(ctx, tx, d, baseURL, true); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE library_sources SET last_sync_started_at=$2,last_sync_succeeded_at=$2,last_sync_error=NULL,updated_at=now() WHERE id=$1`, d.ID, now); err != nil {
		return fmt.Errorf("update %s sync health: %w", d.Kind, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO source_reconciliation_settings(source_id) VALUES($1) ON CONFLICT(source_id) DO NOTHING`, d.ID); err != nil {
		return fmt.Errorf("ensure %s reconciliation settings: %w", d.Kind, err)
	}
	var allowEmpty bool
	if err = tx.QueryRow(ctx, `SELECT allow_empty_snapshots FROM source_reconciliation_settings WHERE source_id=$1 FOR UPDATE`, d.ID).Scan(&allowEmpty); err != nil {
		return fmt.Errorf("read %s reconciliation settings: %w", d.Kind, err)
	}
	// Empty snapshots are ignored for negative evidence unless an operator opts in.
	reconcile := len(snap.Items) > 0 || allowEmpty
	if reconcile {
		if _, err = tx.Exec(ctx, `INSERT INTO source_snapshot_runs(source_id,observed_at,item_count) VALUES($1,$2,$3)`, d.ID, now, len(snap.Items)); err != nil {
			return fmt.Errorf("record %s snapshot run: %w", d.Kind, err)
		}
	}
	declared := make(map[domain.MediaType]bool, len(d.MediaTypes))
	for _, m := range d.MediaTypes {
		declared[m] = true
	}
	seen := make(map[evidenceKey]struct{}, len(snap.Items))
	for _, item := range snap.Items {
		if strings.TrimSpace(item.SourceID) != d.ID || strings.TrimSpace(item.SourceItemID) == "" || !declared[item.MediaType] {
			return fmt.Errorf("invalid %s inventory item", d.Kind)
		}
		ids, err := json.Marshal(item.ExternalIDs)
		if err != nil {
			return fmt.Errorf("encode %s provider IDs: %w", d.Kind, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO inventory_items(id,source_id,source_item_id,media_type,title,year,platform_key,external_ids,first_seen_at,last_seen_at,added_at,artwork_ref) VALUES($1,$2,$3,$4,$5,NULLIF($6,0),NULLIF($7,''),$8,$9,$9,$10,$11) ON CONFLICT(source_id,source_item_id) DO UPDATE SET media_type=EXCLUDED.media_type,title=EXCLUDED.title,year=EXCLUDED.year,platform_key=EXCLUDED.platform_key,external_ids=EXCLUDED.external_ids,last_seen_at=EXCLUDED.last_seen_at,added_at=COALESCE(EXCLUDED.added_at,inventory_items.added_at),artwork_ref=EXCLUDED.artwork_ref`, newUUID(), d.ID, item.SourceItemID, item.MediaType, item.Title, item.Year, item.PlatformKey, ids, now, nullTime(item.AddedAt), item.ArtworkRef); err != nil {
			return fmt.Errorf("upsert %s inventory item: %w", d.Kind, err)
		}
		matched, err := matchCatalogItems(ctx, tx, d, item)
		if err != nil {
			return err
		}
		if len(matched) == 0 {
			if err := suggestMatches(ctx, tx, item, now); err != nil {
				return err
			}
			matched, err = acceptedMatches(ctx, tx, item, now)
			if err != nil {
				return err
			}
		}
		for _, catalogID := range matched {
			seen[evidenceKey{catalogID, item.PlatformKey}] = struct{}{}
			if err := recordPresence(ctx, tx, d, catalogID, item.SourceItemID, item.PlatformKey, now); err != nil {
				return err
			}
		}
	}
	if reconcile {
		if err = reconcileAbsences(ctx, tx, d, now, seen); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s inventory snapshot: %w", d.Kind, err)
	}
	return nil
}

// evidenceKey identifies one piece of availability evidence from a Source: the
// catalog item and the platform it was observed on ("" when not applicable).
type evidenceKey struct{ catalogID, platformKey string }

// matchCatalogItems finds the catalog items an inventory item's identifiers
// name. Only identifier kinds the Source declares are trusted; each is a
// catalog provider ("tmdb"), and kinds with no matching provider match nothing.
func matchCatalogItems(ctx context.Context, tx pgx.Tx, d inventory.Descriptor, item inventory.Item) ([]string, error) {
	declared := make(map[string]bool, len(d.IdentifierKinds))
	for _, kind := range d.IdentifierKinds {
		declared[kind] = true
	}
	kinds := make([]string, 0, len(item.ExternalIDs))
	for kind, value := range item.ExternalIDs {
		if declared[kind] && strings.TrimSpace(value) != "" {
			kinds = append(kinds, kind)
		}
	}
	sort.Strings(kinds)
	var matched []string
	have := map[string]bool{}
	for _, kind := range kinds {
		// Either the identifier is the catalog item's canonical one (kind is the
		// provider), or it was recorded as an alias of it.
		rows, err := tx.Query(ctx, `SELECT id::text FROM catalog_items WHERE provider=$1 AND media_type=$2 AND provider_id=$3
			UNION
			SELECT a.catalog_item_id::text FROM catalog_item_aliases a JOIN catalog_items c ON c.id=a.catalog_item_id WHERE a.kind=$1 AND a.value=$3 AND c.media_type=$2`, kind, item.MediaType, strings.TrimSpace(item.ExternalIDs[kind]))
		if err != nil {
			return nil, fmt.Errorf("match inventory item: %w", err)
		}
		for rows.Next() {
			var catalogID string
			if err := rows.Scan(&catalogID); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan matched catalog item: %w", err)
			}
			if !have[catalogID] {
				have[catalogID] = true
				matched = append(matched, catalogID)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("match inventory item: %w", err)
		}
	}
	return matched, nil
}

// recordPresence is positive evidence from one Source: it clears that Source's
// absence streak, records the evidence, makes the item available and moves its
// open Requests (D014).
func recordPresence(ctx context.Context, tx pgx.Tx, d inventory.Descriptor, catalogID, sourceItemID, platformKey string, now time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM source_absence_state WHERE source_id=$1 AND catalog_item_id=$2 AND platform_key=$3`, d.ID, catalogID, platformKey); err != nil {
		return fmt.Errorf("clear %s absence streak: %w", d.Kind, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO availability_evidence(catalog_item_id,source_id,source_item_id,platform_key,observed_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(catalog_item_id,source_id,platform_key) DO UPDATE SET source_item_id=EXCLUDED.source_item_id,observed_at=EXCLUDED.observed_at`, catalogID, d.ID, sourceItemID, platformKey, now); err != nil {
		return fmt.Errorf("upsert availability evidence: %w", err)
	}
	var prior string
	var version int64
	if err := tx.QueryRow(ctx, `SELECT availability,availability_version FROM catalog_items WHERE id=$1 FOR UPDATE`, catalogID).Scan(&prior, &version); err != nil {
		return fmt.Errorf("read catalog availability: %w", err)
	}
	if prior != "available" {
		version++
		if _, err := tx.Exec(ctx, `UPDATE catalog_items SET availability='available',availability_version=$2,updated_at=$3 WHERE id=$1`, catalogID, version, now); err != nil {
			return fmt.Errorf("mark catalog item available: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO availability_events(id,catalog_item_id,idempotency_key,event_type,data,created_at) VALUES($1,$2,$3,'availability.available','{}'::jsonb,$4) ON CONFLICT(idempotency_key) DO NOTHING`, newUUID(), catalogID, fmt.Sprintf("availability:%s:%d", catalogID, version), now); err != nil {
			return fmt.Errorf("append availability event: %w", err)
		}
	}
	return markOpenRequestsAvailable(ctx, tx, catalogID, d.Kind, now)
}

// reconcileAbsences applies one Source's complete sync to the items it used to
// have. An item missing from the Source's snapshot starts a streak; two
// complete syncs at least a day apart end it by dropping that Source's
// evidence. Only when no enabled covering Source still has evidence does the
// item become missing (D015), so one Source dropping an item never hides it
// while another still holds it.
func reconcileAbsences(ctx context.Context, tx pgx.Tx, d inventory.Descriptor, now time.Time, seen map[evidenceKey]struct{}) error {
	rows, err := tx.Query(ctx, `SELECT c.id::text,e.platform_key,c.availability,c.availability_version,a.first_absent_at,a.last_absent_at,COALESCE(a.absence_count,0)
		FROM availability_evidence e JOIN catalog_items c ON c.id=e.catalog_item_id
		LEFT JOIN source_absence_state a ON a.source_id=e.source_id AND a.catalog_item_id=c.id AND a.platform_key=e.platform_key
		WHERE e.source_id=$1 ORDER BY c.id,e.platform_key FOR UPDATE OF c`, d.ID)
	if err != nil {
		return fmt.Errorf("list %s availability candidates: %w", d.Kind, err)
	}
	type candidate struct {
		id, platformKey, availability string
		version, count                int64
		first, last                   *time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.platformKey, &c.availability, &c.version, &c.first, &c.last, &c.count); err != nil {
			rows.Close()
			return fmt.Errorf("scan %s availability candidate: %w", d.Kind, err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate %s availability candidates: %w", d.Kind, err)
	}
	rows.Close()
	for _, c := range candidates {
		if _, ok := seen[evidenceKey{c.id, c.platformKey}]; ok {
			continue
		}
		if c.first == nil {
			if _, err := tx.Exec(ctx, `INSERT INTO source_absence_state(source_id,catalog_item_id,platform_key,first_absent_at,last_absent_at,absence_count) VALUES($1,$2,$3,$4,$4,1) ON CONFLICT(source_id,catalog_item_id,platform_key) DO NOTHING`, d.ID, c.id, c.platformKey, now); err != nil {
				return fmt.Errorf("record first %s absence: %w", d.Kind, err)
			}
			continue
		}
		if !now.After(*c.last) || now.Before(c.first.Add(absenceInterval)) {
			continue
		}
		// This Source has now completed enough syncs without the item: drop its evidence.
		if _, err := tx.Exec(ctx, `DELETE FROM availability_evidence WHERE catalog_item_id=$1 AND source_id=$2 AND platform_key=$3`, c.id, d.ID, c.platformKey); err != nil {
			return fmt.Errorf("remove stale %s availability evidence: %w", d.Kind, err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM source_absence_state WHERE catalog_item_id=$1 AND source_id=$2 AND platform_key=$3`, c.id, d.ID, c.platformKey); err != nil {
			return fmt.Errorf("clear finished %s absence streak: %w", d.Kind, err)
		}
		var covered bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM availability_evidence e JOIN library_sources s ON s.id=e.source_id JOIN catalog_items ci ON ci.id=e.catalog_item_id WHERE e.catalog_item_id=$1 AND s.enabled AND s.authoritative_media_types ? ci.media_type)`, c.id).Scan(&covered); err != nil {
			return fmt.Errorf("check remaining availability evidence: %w", err)
		}
		if covered || c.availability != "available" {
			continue
		}
		version := c.version + 1
		if _, err := tx.Exec(ctx, `UPDATE catalog_items SET availability='missing',availability_version=$2,updated_at=$3 WHERE id=$1 AND availability='available'`, c.id, version, now); err != nil {
			return fmt.Errorf("mark catalog item missing: %w", err)
		}
		key := fmt.Sprintf("availability:%s:%d", c.id, version)
		if _, err := tx.Exec(ctx, `INSERT INTO availability_events(id,catalog_item_id,idempotency_key,event_type,data,created_at) VALUES($1,$2,$3,'availability.missing','{}'::jsonb,$4) ON CONFLICT(idempotency_key) DO NOTHING`, newUUID(), c.id, key, now); err != nil {
			return fmt.Errorf("append missing availability event: %w", err)
		}
		if err := enqueueGlobalAppriseEvent(ctx, tx, key, "Availability changed", "A requested item is no longer available."); err != nil {
			return err
		}
	}
	return nil
}

// observedPlatforms lists the platforms the item is currently evidenced on by
// enabled Sources covering its media type ("" for a platform a Source could not identify).
func observedPlatforms(ctx context.Context, tx pgx.Tx, catalogID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT e.platform_key FROM availability_evidence e JOIN library_sources s ON s.id=e.source_id JOIN catalog_items c ON c.id=e.catalog_item_id WHERE e.catalog_item_id=$1 AND s.enabled AND s.authoritative_media_types ? c.media_type`, catalogID)
	if err != nil {
		return nil, fmt.Errorf("list observed platforms: %w", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("scan observed platform: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// markOpenRequestsAvailable is the RequestAvailable path (D014): each open
// Request for the catalog item becomes available with a version bump, a
// sequenced audit event and one notification per subscriber plus one global
// Apprise event, all keyed by Request so re-running a sync adds nothing.
func markOpenRequestsAvailable(ctx context.Context, tx pgx.Tx, catalogID, source string, now time.Time) error {
	rows, err := tx.Query(ctx, `SELECT r.id::text,r.status,r.version,c.title,c.media_type,r.acceptable_platforms FROM requests r JOIN catalog_items c ON c.id=r.catalog_item_id
		WHERE r.catalog_item_id=$1 AND r.status IN ('requested','approved') ORDER BY r.id FOR UPDATE OF r`, catalogID)
	if err != nil {
		return fmt.Errorf("list open requests for available item: %w", err)
	}
	type open struct {
		id, status, title string
		media             domain.MediaType
		platforms         domain.PlatformSelection
		version           int64
	}
	var opens []open
	for rows.Next() {
		var o open
		var criteria []byte
		if err := rows.Scan(&o.id, &o.status, &o.version, &o.title, &o.media, &criteria); err != nil {
			rows.Close()
			return fmt.Errorf("scan open request: %w", err)
		}
		if err := json.Unmarshal(criteria, &o.platforms); err != nil {
			rows.Close()
			return fmt.Errorf("decode request platforms: %w", err)
		}
		opens = append(opens, o)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate open requests: %w", err)
	}
	rows.Close()
	// A game Request names the platforms it accepts, so the game being present
	// somewhere is not enough: the evidence must satisfy the selection (D009).
	var observed []string
	for _, o := range opens {
		if o.media == domain.MediaGame {
			if observed, err = observedPlatforms(ctx, tx, catalogID); err != nil {
				return err
			}
			break
		}
	}
	var satisfied []open
	for _, o := range opens {
		if o.media != domain.MediaGame || o.platforms.SatisfiedBy(observed) {
			satisfied = append(satisfied, o)
		}
	}
	opens = satisfied
	for _, o := range opens {
		if _, err := tx.Exec(ctx, `UPDATE requests SET status='available',available_at=$2,availability_state='available',updated_at=$2,version=version+1 WHERE id=$1`, o.id, now); err != nil {
			return fmt.Errorf("mark request available: %w", err)
		}
		var sequence int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(aggregate_sequence),0)+1 FROM request_events WHERE request_id=$1`, o.id).Scan(&sequence); err != nil {
			return fmt.Errorf("sequence request audit event: %w", err)
		}
		data := mustJSON(map[string]any{"from": o.status, "to": domain.RequestAvailable, "version": o.version + 1, "source": source})
		var eventID string
		if err := tx.QueryRow(ctx, `INSERT INTO request_events(id,request_id,event_type,data,created_at,aggregate_sequence) VALUES($1,$2,'request.available',$3,$4,$5) RETURNING id::text`, newUUID(), o.id, data, now, sequence).Scan(&eventID); err != nil {
			return fmt.Errorf("append request audit event: %w", err)
		}
		key := "request.available:" + o.id
		const body = "A requested item is now available."
		if _, err := tx.Exec(ctx, `INSERT INTO notification_deliveries(id,event_key,request_event_id,user_id,channel,status,title,body,created_at,available_at,updated_at)
			SELECT gen_random_uuid(),$2,$3,s.user_id,'in_app','pending',$4,$5,now(),now(),now() FROM request_subscriptions s WHERE s.request_id=$1
			ON CONFLICT(event_key,user_id,channel) WHERE event_key IS NOT NULL DO NOTHING`, o.id, key, eventID, o.title, body); err != nil {
			return fmt.Errorf("enqueue availability inbox notifications: %w", err)
		}
		if err := enqueueGlobalAppriseEvent(ctx, tx, key, o.title, body); err != nil {
			return err
		}
	}
	return nil
}

// RecordSyncFailure updates operational health without touching inventory or availability.
func (s *Store) RecordSyncFailure(ctx context.Context, d inventory.Descriptor, started time.Time, safeError string) error {
	if safeError == "" {
		safeError = "sync failed"
	}
	if len(safeError) > 256 {
		safeError = safeError[:256]
	}
	if err := upsertSource(ctx, s.pool, d, "", false); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE library_sources SET last_sync_started_at=$2,last_sync_error=$3,updated_at=now() WHERE id=$1`, d.ID, started, safeError); err != nil {
		return fmt.Errorf("record %s sync failure: %w", d.Kind, err)
	}
	return nil
}

// execer is the part of pgx.Tx and the pool that upsertSource needs.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// upsertSource is the one write of a Source's identity row. A successful sync
// (overwrite) refreshes name, URL and media types; a failure only makes sure the
// row exists, so it can never replace a URL a past success recorded.
func upsertSource(ctx context.Context, q execer, d inventory.Descriptor, baseURL string, overwrite bool) error {
	mediaTypes := make([]string, len(d.MediaTypes))
	for i, m := range d.MediaTypes {
		mediaTypes[i] = string(m)
	}
	conflict := `DO NOTHING`
	if overwrite {
		conflict = `DO UPDATE SET kind=EXCLUDED.kind,name=EXCLUDED.name,base_url=EXCLUDED.base_url,enabled=true,authoritative_media_types=EXCLUDED.authoritative_media_types,updated_at=now()`
	}
	if _, err := q.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,enabled,authoritative_media_types,created_at,updated_at) VALUES($1,$2,$3,$4,true,$5,now(),now()) ON CONFLICT(id) `+conflict, d.ID, d.Kind, d.Name, baseURL, mustJSON(mediaTypes)); err != nil {
		return fmt.Errorf("upsert %s source: %w", d.Kind, err)
	}
	return nil
}

// LastSnapshotTimes maps each Source ID to the time of its last successful
// snapshot. Run outcomes live in job_runs; this is the other half of Sync health.
func (s *Store) LastSnapshotTimes(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text,last_sync_succeeded_at FROM library_sources WHERE last_sync_succeeded_at IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("list last snapshot times: %w", err)
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("scan last snapshot time: %w", err)
		}
		out[id] = at
	}
	return out, rows.Err()
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// matchExistingInventory records positive evidence for a catalog item from the
// inventory already stored: items in a source's latest complete snapshot whose
// provider id (or a recorded alias) is the catalog item's. It is the other half
// of sync-time matching, for items resolved after their library last synced.
func matchExistingInventory(ctx context.Context, tx pgx.Tx, catalogID, mediaType, provider, providerID string, now time.Time) error {
	rows, err := tx.Query(ctx, `SELECT DISTINCT i.source_id::text,src.kind,i.source_item_id,COALESCE(i.platform_key,'')
		FROM inventory_items i
		JOIN library_sources src ON src.id=i.source_id AND src.enabled
		WHERE i.media_type=$2
		  AND i.last_seen_at >= COALESCE((SELECT max(observed_at) FROM source_snapshot_runs r WHERE r.source_id=i.source_id), i.last_seen_at)
		  AND (i.external_ids->>$3 = $4
		       OR EXISTS (SELECT 1 FROM catalog_item_aliases a WHERE a.catalog_item_id=$1::uuid AND i.external_ids->>a.kind = a.value))`,
		catalogID, mediaType, provider, providerID)
	if err != nil {
		return fmt.Errorf("match catalog item to inventory: %w", err)
	}
	type hit struct{ sourceID, kind, itemID, platform string }
	var hits []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.sourceID, &h.kind, &h.itemID, &h.platform); err != nil {
			rows.Close()
			return fmt.Errorf("scan inventory match: %w", err)
		}
		hits = append(hits, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("match catalog item to inventory: %w", err)
	}
	for _, h := range hits {
		if err := recordPresence(ctx, tx, inventory.Descriptor{ID: h.sourceID, Kind: h.kind}, catalogID, h.itemID, h.platform, now); err != nil {
			return err
		}
	}
	return nil
}
