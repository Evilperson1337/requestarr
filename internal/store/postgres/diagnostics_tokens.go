package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
)

// diagnosticsTokenLockKey serializes creation so the five-token limit holds
// when several creations race.
const diagnosticsTokenLockKey = 0x72647174 // "rdqt"

var _ auth.DiagnosticsTokenStore = (*Store)(nil)

func insertDiagnosticsAudit(ctx context.Context, tx pgx.Tx, e auth.AuditEvent, tokenID string) error {
	var user, addr any
	if e.UserID != "" {
		user = e.UserID
	}
	if e.RemoteAddr != "" {
		addr = e.RemoteAddr
	}
	_, err := tx.Exec(ctx, `INSERT INTO auth_audit_events(event_type,user_id,remote_addr,token_id,created_at,count) VALUES($1,$2::uuid,$3,$4::uuid,$5,1)`, e.Type, user, addr, tokenID, e.At)
	return err
}

func (s *Store) CreateDiagnosticsToken(ctx context.Context, t auth.DiagnosticsToken, hash []byte, audit auth.AuditEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin diagnostics token: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, diagnosticsTokenLockKey); err != nil {
		return fmt.Errorf("lock diagnostics tokens: %w", err)
	}
	var active int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM diagnostics_tokens WHERE revoked_at IS NULL AND expires_at > $1`, t.CreatedAt).Scan(&active); err != nil {
		return fmt.Errorf("count diagnostics tokens: %w", err)
	}
	if active >= auth.MaxActiveDiagnosticsTokens {
		return auth.ErrDiagnosticsTokenLimit
	}
	if _, err := tx.Exec(ctx, `INSERT INTO diagnostics_tokens(id,name,created_by,created_at,expires_at,token_hash) VALUES($1::uuid,$2,$3::uuid,$4,$5,$6)`, t.ID, t.Name, t.CreatedBy, t.CreatedAt, t.ExpiresAt, hash); err != nil {
		return fmt.Errorf("insert diagnostics token: %w", err)
	}
	if err := insertDiagnosticsAudit(ctx, tx, audit, t.ID); err != nil {
		return fmt.Errorf("audit diagnostics token: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Store) ListDiagnosticsTokens(ctx context.Context) ([]auth.DiagnosticsToken, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text,name,created_by::text,created_at,expires_at,last_used_at,revoked_at FROM diagnostics_tokens WHERE revoked_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list diagnostics tokens: %w", err)
	}
	defer rows.Close()
	out := []auth.DiagnosticsToken{}
	for rows.Next() {
		var t auth.DiagnosticsToken
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedBy, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan diagnostics token: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) RevokeDiagnosticsToken(ctx context.Context, id string, now time.Time, audit auth.AuditEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke: %w", err)
	}
	defer tx.Rollback(ctx)
	var got string
	err = tx.QueryRow(ctx, `UPDATE diagnostics_tokens SET revoked_at=$2 WHERE id=$1::uuid AND revoked_at IS NULL RETURNING id::text`, id, now).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrDiagnosticsTokenNotFound
	}
	if err != nil {
		// A malformed id is a token that does not exist, not a server fault.
		if isInvalidTextRepresentation(err) {
			return auth.ErrDiagnosticsTokenNotFound
		}
		return fmt.Errorf("revoke diagnostics token: %w", err)
	}
	if err := insertDiagnosticsAudit(ctx, tx, audit, got); err != nil {
		return fmt.Errorf("audit revoke: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Store) DiagnosticsTokenByHash(ctx context.Context, hash []byte) (auth.DiagnosticsTokenRecord, error) {
	var r auth.DiagnosticsTokenRecord
	err := s.pool.QueryRow(ctx, `SELECT t.id::text,t.name,t.created_by::text,t.created_at,t.expires_at,t.last_used_at,t.revoked_at,
		COALESCE(u.role IN (`+domain.SQLRoleList(domain.RolesThatManageAccess())+`) AND NOT COALESCE(lc.disabled,FALSE),FALSE)
		FROM diagnostics_tokens t LEFT JOIN users u ON u.id=t.created_by LEFT JOIN local_credentials lc ON lc.user_id=u.id
		WHERE t.token_hash=$1`, hash).Scan(&r.ID, &r.Name, &r.CreatedBy, &r.CreatedAt, &r.ExpiresAt, &r.LastUsedAt, &r.RevokedAt, &r.CreatorCanSignIn)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.DiagnosticsTokenRecord{}, auth.ErrDiagnosticsTokenNotFound
	}
	if err != nil {
		return auth.DiagnosticsTokenRecord{}, fmt.Errorf("find diagnostics token: %w", err)
	}
	return r, nil
}

func (s *Store) TouchDiagnosticsToken(ctx context.Context, id string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE diagnostics_tokens SET last_used_at=$2::timestamptz WHERE id=$1::uuid AND (last_used_at IS NULL OR last_used_at <= $2::timestamptz - ($3::float8 * interval '1 second'))`, id, now, auth.DiagnosticsTokenTouchInterval.Seconds())
	if err != nil {
		return fmt.Errorf("touch diagnostics token: %w", err)
	}
	return nil
}

func isInvalidTextRepresentation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "22P02"
}
