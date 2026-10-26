package postgres

import (
	"context"
	"fmt"

	"github.com/evilperson1337/requestarr/internal/secrets"
)

var _ secrets.Repository = (*Store)(nil)

func (s *Store) ListStoredSecrets(ctx context.Context) ([]secrets.Stored, error) {
	rows, err := s.pool.Query(ctx, `SELECT name,ciphertext,key_id,updated_at FROM stored_secrets ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query stored secrets: %w", err)
	}
	defer rows.Close()
	var out []secrets.Stored
	for rows.Next() {
		var r secrets.Stored
		var name string
		if err := rows.Scan(&name, &r.Ciphertext, &r.KeyID, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan stored secret: %w", err)
		}
		r.Name = secrets.Name(name)
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutStoredSecret upserts the encrypted row and records a value-free audit
// event in the same transaction.
func (s *Store) PutStoredSecret(ctx context.Context, actorID string, name secrets.Name, sealed secrets.Sealed) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO stored_secrets(name,ciphertext,key_id,updated_by,updated_at) VALUES($1,$2,$3,$4,now())
		ON CONFLICT (name) DO UPDATE SET ciphertext=EXCLUDED.ciphertext,key_id=EXCLUDED.key_id,updated_by=EXCLUDED.updated_by,updated_at=now()`,
		string(name), sealed.Ciphertext, sealed.KeyID, actorID); err != nil {
		return fmt.Errorf("store secret: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO secret_audit_events(actor_user_id,secret_name,action) VALUES($1,$2,'set')`, actorID, string(name)); err != nil {
		return fmt.Errorf("audit secret: %w", err)
	}
	return tx.Commit(ctx)
}

// DeleteStoredSecret removes the row and records a value-free audit event in the
// same transaction. Clearing an absent secret records nothing.
func (s *Store) DeleteStoredSecret(ctx context.Context, actorID string, name secrets.Name) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `DELETE FROM stored_secrets WHERE name=$1`, string(name))
	if err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO secret_audit_events(actor_user_id,secret_name,action) VALUES($1,$2,'clear')`, actorID, string(name)); err != nil {
			return fmt.Errorf("audit secret: %w", err)
		}
	}
	return tx.Commit(ctx)
}
