package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/jackc/pgx/v5"
)

// The built-in local administrator's storage (D040). The account is an
// ordinary admin users row marked builtin, with the fixed oidc_subject
// 'local:admin' (OIDC subjects are stored as 'oidc:<digest>', so they cannot
// collide), plus a local_credentials row.

const localAdminSubject = "local:admin"

var (
	_ auth.LocalStore     = (*Store)(nil)
	_ auth.BootstrapStore = (*Store)(nil)
)

func (s *Store) localCredential(ctx context.Context, where string, arg string) (auth.LocalCredential, error) {
	var c auth.LocalCredential
	err := s.pool.QueryRow(ctx, `SELECT user_id::text,username,password_hash,must_change,disabled FROM local_credentials WHERE `+where, arg).
		Scan(&c.UserID, &c.Username, &c.PasswordHash, &c.MustChange, &c.Disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.LocalCredential{}, auth.ErrNoCredential
	}
	return c, err
}

func (s *Store) LocalCredentialByUsername(ctx context.Context, username string) (auth.LocalCredential, error) {
	return s.localCredential(ctx, `username=$1`, username)
}

func (s *Store) LocalCredentialByUserID(ctx context.Context, userID string) (auth.LocalCredential, error) {
	if len(userID) != 36 { // not a UUID: no such credential, and no cast error
		return auth.LocalCredential{}, auth.ErrNoCredential
	}
	return s.localCredential(ctx, `user_id=$1::uuid`, userID)
}

// CreateLocalAdmin creates the built-in administrator with must-change set,
// but only while there are no users at all. The advisory lock serializes
// concurrent starts, so exactly one creates it.
func (s *Store) CreateLocalAdmin(ctx context.Context, hash string, now time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, adminRoleAdvisoryLockID); err != nil {
		return false, fmt.Errorf("lock admin creation: %w", err)
	}
	var users int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users); err != nil {
		return false, err
	}
	if users > 0 {
		return false, nil
	}
	id := newUUID()
	if _, err = tx.Exec(ctx, `INSERT INTO users(id,oidc_subject,email,display_name,role,created_at,updated_at,builtin) VALUES($1,$2,NULL,'Administrator','admin',$3,$3,TRUE)`, id, localAdminSubject, now); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO local_credentials(user_id,username,password_hash,must_change,created_at,updated_at) VALUES($1,$2,$3,TRUE,$4,$4)`, id, auth.LocalUsername, hash, now); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE setup_state SET complete=TRUE,claimed_by=$1,completed_at=$2,token_hash=NULL,token_expires_at=NULL WHERE singleton=TRUE AND complete=FALSE`, id, now); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_audit_events(event_type,user_id,username,created_at) VALUES('admin_created',$1,$2,$3)`, id, auth.LocalUsername, now); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// ChangeLocalPassword stores the new hash, clears must-change and ends every
// other session of the user in one transaction. The session with token hash
// keep stays.
func (s *Store) ChangeLocalPassword(ctx context.Context, userID, hash string, keep []byte, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE local_credentials SET password_hash=$2,must_change=FALSE,password_changed_at=$3,updated_at=$3 WHERE user_id=$1::uuid`, userID, hash, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return auth.ErrNoCredential
	}
	if _, err = tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=$2 WHERE user_id=$1::uuid AND revoked_at IS NULL AND token_hash<>$3`, userID, now, keep); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RecordAuthEvent appends one authentication audit row.
func (s *Store) RecordAuthEvent(ctx context.Context, e auth.AuditEvent) error {
	var user, name, addr any
	if e.UserID != "" {
		user = e.UserID
	}
	if e.Username != "" {
		name = e.Username
	}
	if e.RemoteAddr != "" {
		addr = e.RemoteAddr
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO auth_audit_events(event_type,user_id,username,remote_addr,created_at) VALUES($1,$2::uuid,$3,$4,$5)`, e.Type, user, name, addr, e.At)
	return err
}
