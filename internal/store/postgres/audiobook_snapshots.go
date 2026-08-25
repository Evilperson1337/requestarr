package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
)

var (
	ErrAudiobookSnapshotUnavailable = errors.New("audiobook snapshot repository unavailable")
	ErrAudiobookSnapshotInvalid     = errors.New("audiobook snapshot input is invalid")
	ErrAudiobookSnapshotProvenance  = errors.New("audiobook snapshot source not found")
)

// AudiobookSnapshotRepository atomically persists complete local inventory.
// It contains no reader or source configuration behavior.
type AudiobookSnapshotRepository struct{ pool *Pool }

func NewAudiobookSnapshotRepository(pool *Pool) *AudiobookSnapshotRepository {
	return &AudiobookSnapshotRepository{pool: pool}
}

func (r *AudiobookSnapshotRepository) CommitAudiobookSnapshot(ctx context.Context, snapshot inventory.AudiobookSnapshot) (inventory.SnapshotCommit, error) {
	if err := inventory.ValidateAudiobookSnapshot(snapshot); err != nil {
		return inventory.SnapshotCommit{}, fmt.Errorf("%w: %v", ErrAudiobookSnapshotInvalid, err)
	}
	if err := ctx.Err(); err != nil {
		return inventory.SnapshotCommit{}, fmt.Errorf("%w: %v", ErrAudiobookSnapshotUnavailable, err)
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return inventory.SnapshotCommit{}, ErrAudiobookSnapshotUnavailable
	}
	hash, err := snapshotHash(snapshot)
	if err != nil {
		return inventory.SnapshotCommit{}, fmt.Errorf("%w: encode normalized snapshot", ErrAudiobookSnapshotInvalid)
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return inventory.SnapshotCommit{}, classifyAudiobookSnapshotError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, snapshot.SourceID); err != nil {
		return inventory.SnapshotCommit{}, classifyAudiobookSnapshotError(ctx, err)
	}
	var existingID string
	err = tx.QueryRow(ctx, `SELECT id FROM audiobook_inventory_snapshots WHERE source_id=$1 AND content_hash=$2`, snapshot.SourceID, hash).Scan(&existingID)
	if err == nil {
		if err = tx.Commit(ctx); err != nil {
			return inventory.SnapshotCommit{}, classifyAudiobookSnapshotError(ctx, err)
		}
		return inventory.SnapshotCommit{SnapshotID: existingID}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return inventory.SnapshotCommit{}, classifyAudiobookSnapshotError(ctx, err)
	}
	var latestID string
	var latestCompletedAt time.Time
	err = tx.QueryRow(ctx, `SELECT id, completed_at FROM audiobook_inventory_snapshots WHERE source_id=$1 ORDER BY completed_at DESC, committed_at DESC LIMIT 1`, snapshot.SourceID).Scan(&latestID, &latestCompletedAt)
	if err == nil && !snapshot.CompletedAt.UTC().After(latestCompletedAt) {
		if err = tx.Commit(ctx); err != nil {
			return inventory.SnapshotCommit{}, classifyAudiobookSnapshotError(ctx, err)
		}
		return inventory.SnapshotCommit{SnapshotID: latestID}, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return inventory.SnapshotCommit{}, classifyAudiobookSnapshotError(ctx, err)
	}

	snapshotID, err := newUUID()
	if err != nil {
		return inventory.SnapshotCommit{}, fmt.Errorf("%w: generate snapshot ID", ErrAudiobookSnapshotUnavailable)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audiobook_inventory_snapshots(id,source_id,content_hash,completed_at,committed_at) VALUES ($1,$2,$3,$4,clock_timestamp())`, snapshotID, snapshot.SourceID, hash, snapshot.CompletedAt.UTC()); err != nil {
		return inventory.SnapshotCommit{}, classifyAudiobookSnapshotError(ctx, err)
	}
	for _, library := range snapshot.SelectedLibrary {
		if _, err = tx.Exec(ctx, `INSERT INTO audiobook_source_libraries(source_id,remote_library_id,name,created_at) VALUES ($1,$2,$3,clock_timestamp()) ON CONFLICT (source_id,remote_library_id) DO UPDATE SET name=EXCLUDED.name`, snapshot.SourceID, library.ID, library.Name); err != nil {
			return inventory.SnapshotCommit{}, classifyAudiobookSnapshotError(ctx, err)
		}
	}
	for _, item := range snapshot.Items {
		if err = upsertAudiobookItem(ctx, tx, snapshot, snapshotID, item); err != nil {
			return inventory.SnapshotCommit{}, classifyAudiobookSnapshotError(ctx, err)
		}
	}
	// This executes only after every item has been accepted, inside the same
	// transaction: incomplete or failed snapshots never create tombstones.
	if _, err = tx.Exec(ctx, `UPDATE inventory_items SET tombstoned_at=$3 WHERE source_id=$1 AND media_type='audiobook' AND tombstoned_at IS NULL AND (last_snapshot_id IS NULL OR last_snapshot_id <> $2)`, snapshot.SourceID, snapshotID, snapshot.CompletedAt.UTC()); err != nil {
		return inventory.SnapshotCommit{}, classifyAudiobookSnapshotError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return inventory.SnapshotCommit{}, classifyAudiobookSnapshotError(ctx, err)
	}
	return inventory.SnapshotCommit{SnapshotID: snapshotID, Committed: true}, nil
}

func upsertAudiobookItem(ctx context.Context, tx pgx.Tx, snapshot inventory.AudiobookSnapshot, snapshotID string, item inventory.AudiobookItem) error {
	id, err := newUUID()
	if err != nil {
		return err
	}
	var inventoryID string
	externalURL, err := inventory.NormalizeExternalURL(item.ExternalURL)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `INSERT INTO inventory_items(id,source_id,source_item_id,media_type,title,external_ids,external_url,first_seen_at,last_seen_at,last_snapshot_id,tombstoned_at)
VALUES ($1,$2,$3,'audiobook',$4,'{}'::jsonb,$5,$6,$6,$7,NULL)
ON CONFLICT (source_id,source_item_id) DO UPDATE SET title=EXCLUDED.title,external_url=EXCLUDED.external_url,last_seen_at=EXCLUDED.last_seen_at,last_snapshot_id=EXCLUDED.last_snapshot_id,tombstoned_at=NULL
RETURNING id`, id, snapshot.SourceID, item.SourceItemID, item.Title, nullIfEmpty(externalURL), snapshot.CompletedAt.UTC(), snapshotID).Scan(&inventoryID)
	if err != nil {
		return err
	}
	details, err := domain.NormalizeAudiobookDetails(item.Details)
	if err != nil {
		return err
	}
	authors, err := json.Marshal(details.Authors)
	if err != nil {
		return err
	}
	narrators, err := json.Marshal(details.Narrators)
	if err != nil {
		return err
	}
	alternateIDs, err := json.Marshal(details.AlternateIDs)
	if err != nil {
		return err
	}
	var releaseDate any
	if details.ReleaseDate != "" {
		releaseDate = details.ReleaseDate
	}
	_, err = tx.Exec(ctx, `INSERT INTO audiobook_inventory_details(inventory_item_id,asin,audible_region,authors,narrators,series,series_position,runtime_seconds,language,publisher,release_date,edition_type,abridged,alternate_ids)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (inventory_item_id) DO UPDATE SET asin=EXCLUDED.asin,audible_region=EXCLUDED.audible_region,authors=EXCLUDED.authors,narrators=EXCLUDED.narrators,series=EXCLUDED.series,series_position=EXCLUDED.series_position,runtime_seconds=EXCLUDED.runtime_seconds,language=EXCLUDED.language,publisher=EXCLUDED.publisher,release_date=EXCLUDED.release_date,edition_type=EXCLUDED.edition_type,abridged=EXCLUDED.abridged,alternate_ids=EXCLUDED.alternate_ids`, inventoryID, nullIfEmpty(details.ASIN), nullIfEmpty(details.AudibleRegion), authors, narrators, details.Series, details.SeriesPosition, details.RuntimeSeconds, details.Language, details.Publisher, releaseDate, details.EditionType, details.Abridged, alternateIDs)
	return err
}

func snapshotHash(snapshot inventory.AudiobookSnapshot) (string, error) {
	canonical := inventory.CanonicalAudiobookSnapshot(snapshot)
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func classifyAudiobookSnapshotError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %v", ErrAudiobookSnapshotUnavailable, ctx.Err())
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrAudiobookSnapshotProvenance
	}
	return ErrAudiobookSnapshotUnavailable
}

var _ inventory.AudiobookSnapshotRepository = (*AudiobookSnapshotRepository)(nil)
