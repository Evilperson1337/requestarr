package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/evilperson1337/requestarr/internal/domain"
)

// ErrLibraryItemNotFound means no Library item has that id.
var ErrLibraryItemNotFound = errors.New("library item not found")

// ErrBadLibraryCursor means a cursor was not one this server produced.
var ErrBadLibraryCursor = errors.New("invalid library cursor")

const libraryItemColumns = `i.id::text,i.title,COALESCE(i.year,0),i.media_type,src.name,src.kind,COALESCE(i.added_at,i.first_seen_at),i.detail,i.artwork_ref<>'',COALESCE(i.external_url,''),
	(SELECT count(*) FROM issues iss WHERE iss.inventory_item_id=i.id AND iss.status IN ('open','in_progress'))`

func scanLibraryItem(row pgx.Row) (domain.LibraryItem, error) {
	var it domain.LibraryItem
	var hasArt bool
	var added time.Time
	var link string
	var open int64
	if err := row.Scan(&it.ID, &it.Title, &it.Year, &it.MediaType, &it.Source, &it.SourceKind, &added, &it.Detail, &hasArt, &link, &open); err != nil {
		return domain.LibraryItem{}, err
	}
	it.AddedAt, it.OpenIssues, it.IssueKinds = added.UTC(), int(open), domain.IssueKindOptions(it.MediaType)
	if hasArt {
		it.ArtworkURL = "/api/v1/library/items/" + it.ID + "/artwork"
	}
	if safeLibraryLink(link) {
		it.Link = link
	}
	return it, nil
}

func encodeCursor(added time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(added.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeCursor(raw string) (time.Time, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", ErrBadLibraryCursor
	}
	at, id, ok := strings.Cut(string(b), "|")
	t, terr := time.Parse(time.RFC3339Nano, at)
	if !ok || terr != nil || !domain.ValidUUID(id) {
		return time.Time{}, "", ErrBadLibraryCursor
	}
	return t, id, nil
}

// escapeLike makes a search string literal inside LIKE.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// BrowseLibrary lists one media type of what the enabled Sources hold now, newest
// added first. Only items in a Source's latest complete snapshot count, and one
// title in two libraries of the same Source is one entry.
func (s *Store) BrowseLibrary(ctx context.Context, q domain.LibraryQuery) (domain.LibraryPage, error) {
	switch q.Type {
	case domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaGame, domain.MediaAudiobook:
	default:
		return domain.LibraryPage{}, fmt.Errorf("unsupported library media type")
	}
	limit := q.Limit
	if limit < 1 || limit > 100 {
		limit = 48
	}
	args := []any{q.Type}
	where := []string{"i.media_type=$1",
		"i.last_seen_at >= COALESCE((SELECT max(observed_at) FROM source_snapshot_runs r WHERE r.source_id=i.source_id), i.last_seen_at)"}
	if q.Source != "" {
		args = append(args, q.Source)
		where = append(where, fmt.Sprintf("src.kind=$%d", len(args)))
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		args = append(args, "%"+escapeLike(strings.ToLower(search))+"%")
		where = append(where, fmt.Sprintf("lower(i.title) LIKE $%d", len(args)))
	}
	outer := ""
	if q.After != "" {
		at, id, err := decodeCursor(q.After)
		if err != nil {
			return domain.LibraryPage{}, err
		}
		args = append(args, at, id)
		outer = fmt.Sprintf("WHERE (added,id::uuid) < ($%d,$%d::uuid)", len(args)-1, len(args))
	}
	args = append(args, limit+1)
	query := `SELECT * FROM (
		SELECT DISTINCT ON (i.source_id,lower(i.title),COALESCE(i.year,0)) ` + libraryItemColumns + `
		FROM inventory_items i JOIN library_sources src ON src.id=i.source_id AND src.enabled
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY i.source_id,lower(i.title),COALESCE(i.year,0),COALESCE(i.added_at,i.first_seen_at) DESC
	) d(id,title,year,media_type,name,kind,added,detail,has_art,link,open_issues) ` + outer + `
	ORDER BY added DESC,id DESC LIMIT $` + fmt.Sprint(len(args))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return domain.LibraryPage{}, fmt.Errorf("browse library: %w", err)
	}
	defer rows.Close()
	page := domain.LibraryPage{Items: []domain.LibraryItem{}}
	for rows.Next() {
		it, err := scanLibraryItem(rows)
		if err != nil {
			return domain.LibraryPage{}, fmt.Errorf("scan library item: %w", err)
		}
		page.Items = append(page.Items, it)
	}
	if err := rows.Err(); err != nil {
		return domain.LibraryPage{}, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[limit-1]
		cursor := encodeCursor(last.AddedAt, last.ID)
		page.NextCursor = &cursor
	}
	return page, nil
}

// LibraryItem returns one item, whether or not it is in its Source's latest snapshot.
func (s *Store) LibraryItemByID(ctx context.Context, id string) (domain.LibraryItem, error) {
	if !domain.ValidUUID(id) {
		return domain.LibraryItem{}, ErrLibraryItemNotFound
	}
	it, err := scanLibraryItem(s.pool.QueryRow(ctx, `SELECT `+libraryItemColumns+` FROM inventory_items i JOIN library_sources src ON src.id=i.source_id WHERE i.id=$1 AND i.media_type IN ('movie','tv','album','game','audiobook')`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LibraryItem{}, ErrLibraryItemNotFound
	}
	if err != nil || it.MediaType != domain.MediaAlbum {
		return it, err
	}
	rows, err := s.pool.Query(ctx, `SELECT t.id::text,t.title,t.disc,t.position FROM inventory_items t JOIN inventory_items a ON a.source_id=t.source_id AND a.source_item_id=t.parent_source_item_id WHERE a.id=$1 AND t.media_type='song' AND t.parent_source_item_id<>'' ORDER BY t.disc,t.position,lower(t.title) LIMIT 500`, id)
	if err != nil {
		return domain.LibraryItem{}, fmt.Errorf("list album tracks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t domain.LibraryTrack
		if err := rows.Scan(&t.ID, &t.Title, &t.Disc, &t.Position); err != nil {
			return domain.LibraryItem{}, err
		}
		it.Tracks = append(it.Tracks, t)
	}
	return it, rows.Err()
}
