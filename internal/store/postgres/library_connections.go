package postgres

import (
	"context"
	"fmt"

	"github.com/evilperson1337/requestarr/internal/sources"
)

// ListLibraryConnections returns the saved connections by slug.
func (s *Store) ListLibraryConnections(ctx context.Context) (map[string]sources.Connection, error) {
	rows, err := s.pool.Query(ctx, `SELECT slug,url,username FROM library_connections`)
	if err != nil {
		return nil, fmt.Errorf("list library connections: %w", err)
	}
	defer rows.Close()
	out := map[string]sources.Connection{}
	for rows.Next() {
		var c sources.Connection
		if err := rows.Scan(&c.Slug, &c.URL, &c.Username); err != nil {
			return nil, fmt.Errorf("scan library connection: %w", err)
		}
		out[c.Slug] = c
	}
	return out, rows.Err()
}

// PutLibraryConnection saves or replaces one connection.
func (s *Store) PutLibraryConnection(ctx context.Context, c sources.Connection) error {
	if err := c.Validate(); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO library_connections(slug,url,username,updated_at) VALUES($1,$2,$3,now())
		ON CONFLICT (slug) DO UPDATE SET url=EXCLUDED.url,username=EXCLUDED.username,updated_at=now()`, c.Slug, c.URL, c.Username)
	return err
}

// DeleteLibraryConnection removes a saved connection; removing none is not an error.
func (s *Store) DeleteLibraryConnection(ctx context.Context, slug string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM library_connections WHERE slug=$1`, slug)
	return err
}
