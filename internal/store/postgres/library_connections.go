package postgres

import (
	"context"
	"fmt"

	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// SaveLibraryConnection writes a connection (when c is not nil) and its sealed
// credentials in one transaction, each credential with its value-free audit
// event. Either everything is stored or nothing is.
func (s *Store) SaveLibraryConnection(ctx context.Context, actorID string, c *sources.Connection, sealed map[secrets.Name]secrets.Sealed) error {
	if c != nil {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if c != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO library_connections(slug,url,username,updated_at) VALUES($1,$2,$3,now())
			ON CONFLICT (slug) DO UPDATE SET url=EXCLUDED.url,username=EXCLUDED.username,updated_at=now()`, c.Slug, c.URL, c.Username); err != nil {
			return fmt.Errorf("save library connection: %w", err)
		}
	}
	for name, value := range sealed {
		if err := putStoredSecretTx(ctx, tx, actorID, name, value); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

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
