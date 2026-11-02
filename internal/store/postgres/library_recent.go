package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/evilperson1337/requestarr/internal/domain"
)

// ErrNoArtwork means the item is unknown or has no cover reference.
var ErrNoArtwork = errors.New("no artwork")

// RecentlyAdded lists the movies, series and albums libraries gained most
// recently. Only items present in their source's latest complete snapshot count,
// so something since removed never shows. Songs are left out: they would drown the row.
func (s *Store) RecentlyAdded(ctx context.Context, limit int, types []domain.MediaType) ([]domain.RecentLibraryItem, error) {
	if limit < 1 || limit > 50 {
		limit = 12
	}
	wanted := []string{}
	for _, t := range types {
		if t == domain.MediaMovie || t == domain.MediaTV || t == domain.MediaAlbum {
			wanted = append(wanted, string(t))
		}
	}
	if len(wanted) == 0 {
		wanted = []string{"movie", "tv", "album"}
	}
	// The same title in two libraries (a show in both Kids and TV) is one entry: the newest.
	rows, err := s.pool.Query(ctx, `SELECT id,title,year,media_type,name,added,has_art FROM (
		SELECT DISTINCT ON (i.media_type,lower(i.title),COALESCE(i.year,0))
		  i.id::text AS id,i.title,COALESCE(i.year,0) AS year,i.media_type,src.name,COALESCE(i.added_at,i.first_seen_at) AS added,i.artwork_ref<>'' AS has_art
		FROM inventory_items i
		JOIN library_sources src ON src.id=i.source_id AND src.enabled
		WHERE i.media_type = ANY($2)
		  AND i.last_seen_at >= COALESCE((SELECT max(observed_at) FROM source_snapshot_runs r WHERE r.source_id=i.source_id), i.last_seen_at)
		ORDER BY i.media_type,lower(i.title),COALESCE(i.year,0),COALESCE(i.added_at,i.first_seen_at) DESC) d
		ORDER BY added DESC,title
		LIMIT $1`, limit, wanted)
	if err != nil {
		return nil, fmt.Errorf("list recently added: %w", err)
	}
	defer rows.Close()
	out := []domain.RecentLibraryItem{}
	for rows.Next() {
		var item domain.RecentLibraryItem
		var added time.Time
		var hasArt bool
		if err := rows.Scan(&item.ID, &item.Title, &item.Year, &item.MediaType, &item.Source, &added, &hasArt); err != nil {
			return nil, fmt.Errorf("scan recently added: %w", err)
		}
		item.AddedAt = added.UTC()
		if hasArt {
			item.ArtworkURL = "/api/v1/library/items/" + item.ID + "/artwork"
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// LibraryArtworkRef returns the owning source and cover reference of an item.
func (s *Store) LibraryArtworkRef(ctx context.Context, id string) (sourceID, ref string, err error) {
	if !domain.ValidUUID(id) {
		return "", "", ErrNoArtwork
	}
	err = s.pool.QueryRow(ctx, `SELECT source_id::text,artwork_ref FROM inventory_items WHERE id=$1 AND artwork_ref<>''`, id).Scan(&sourceID, &ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNoArtwork
	}
	return sourceID, ref, err
}
