package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/jackc/pgx/v5"
)

// The built-in local administrator's storage (D040). The account is an
// ordinary superuser users row marked builtin, with the fixed oidc_subject
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
// LocalAdminExists reports whether the local administrator's credential is stored.
func (s *Store) LocalAdminExists(ctx context.Context) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_credentials)`).Scan(&exists)
	return exists, err
}

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
	if _, err = tx.Exec(ctx, `INSERT INTO users(id,oidc_subject,email,display_name,role,created_at,updated_at,builtin) VALUES($1,$2,NULL,'Administrator',$4,$3,$3,TRUE)`, id, localAdminSubject, now, domain.RoleSuperuser); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO local_credentials(user_id,username,password_hash,must_change,created_at,updated_at) VALUES($1,$2,$3,TRUE,$4,$4)`, id, auth.LocalUsername, hash, now); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_audit_events(event_type,user_id,username,created_at) VALUES('admin_created',$1,$2,$3)`, id, auth.LocalUsername, now); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// ChangeLocalPassword stores the new hash, clears must-change and ends every
// other session of the user, with its successful audit event, in one
// transaction. The session with token hash keep stays.
func (s *Store) ChangeLocalPassword(ctx context.Context, userID, hash string, keep []byte, remoteAddr string, now time.Time, verify func(storedHash string) bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if verify != nil {
		var stored string
		err = tx.QueryRow(ctx, `SELECT password_hash FROM local_credentials WHERE user_id=$1::uuid FOR UPDATE`, userID).Scan(&stored)
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrNoCredential
		}
		if err != nil {
			return fmt.Errorf("lock local credential: %w", err)
		}
		if !verify(stored) {
			return auth.ErrCurrentPasswordIncorrect
		}
	}
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
	if _, err = tx.Exec(ctx, `INSERT INTO auth_audit_events(event_type,user_id,username,remote_addr,created_at) SELECT $2,user_id,username,$3,$4 FROM local_credentials WHERE user_id=$1::uuid`, userID, auth.AuditPasswordChanged, remoteAddr, now); err != nil {
		return fmt.Errorf("audit local password change: %w", err)
	}
	return tx.Commit(ctx)
}

// ResetLocalAdminPassword replaces the credential and revokes every session,
// serialized with sign-in on the credential row. It turns a disabled account
// back on (shell access is the gate, and the account is the recovery path) but
// does not create one on an upgraded instance. The audit commits atomically.
func (s *Store) ResetLocalAdminPassword(ctx context.Context, hash string, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `UPDATE local_credentials c SET password_hash=$1,must_change=TRUE,disabled=FALSE,password_changed_at=$2,updated_at=$2 FROM users u WHERE c.user_id=u.id AND u.builtin AND c.username=$3 RETURNING c.user_id::text`, hash, now, auth.LocalUsername).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrNoCredential
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=$2 WHERE user_id=$1::uuid AND revoked_at IS NULL`, id, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_audit_events(event_type,user_id,username,created_at) VALUES($1,$2::uuid,$3,$4)`, auth.AuditPasswordChanged, id, auth.LocalUsername, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ResetOrCreateLocalAdmin is the shell recovery path (D040): it resets the
// built-in administrator's password as ResetLocalAdminPassword does, or, when
// the account does not exist (an upgraded instance with no administrator), creates
// it with must-change set and an admin_created audit row. Serialised with the
// other admin-role changes; created reports which one happened.
func (s *Store) ResetOrCreateLocalAdmin(ctx context.Context, hash string, now time.Time) (created bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, adminRoleAdvisoryLockID); err != nil {
		return false, fmt.Errorf("lock admin creation: %w", err)
	}
	var id string
	err = tx.QueryRow(ctx, `UPDATE local_credentials c SET password_hash=$1,must_change=TRUE,disabled=FALSE,password_changed_at=$2,updated_at=$2 FROM users u WHERE c.user_id=u.id AND u.builtin AND c.username=$3 RETURNING c.user_id::text`, hash, now, auth.LocalUsername).Scan(&id)
	switch {
	case err == nil:
		if _, err = tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=$2 WHERE user_id=$1::uuid AND revoked_at IS NULL`, id, now); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO auth_audit_events(event_type,user_id,username,created_at) VALUES($1,$2::uuid,$3,$4)`, auth.AuditPasswordChanged, id, auth.LocalUsername, now); err != nil {
			return false, err
		}
	case errors.Is(err, pgx.ErrNoRows):
		id = newUUID()
		if _, err = tx.Exec(ctx, `INSERT INTO users(id,oidc_subject,email,display_name,role,created_at,updated_at,builtin) VALUES($1,$2,NULL,'Administrator',$4,$3,$3,TRUE)`, id, localAdminSubject, now, domain.RoleSuperuser); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO local_credentials(user_id,username,password_hash,must_change,created_at,updated_at) VALUES($1,$2,$3,TRUE,$4,$4)`, id, auth.LocalUsername, hash, now); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO auth_audit_events(event_type,user_id,username,created_at) VALUES('admin_created',$1,$2,$3)`, id, auth.LocalUsername, now); err != nil {
			return false, err
		}
		created = true
	default:
		return false, err
	}
	return created, tx.Commit(ctx)
}

// RecordAuthEvent appends one authentication audit row.
func (s *Store) RecordAuthEvent(ctx context.Context, e auth.AuditEvent) error {
	_, err := s.RecordAuthEventID(ctx, e)
	return err
}

// AddAuthEventCount adds n attempts to a coalesced failure or throttle row.
func (s *Store) AddAuthEventCount(ctx context.Context, id int64, n int) error {
	_, err := s.pool.Exec(ctx, `UPDATE auth_audit_events SET count=count+$2 WHERE id=$1`, id, n)
	return err
}

// PruneAuthEvents deletes authentication audit rows created before the cutoff.
func (s *Store) PruneAuthEvents(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM auth_audit_events WHERE created_at < $1`, before)
	return tag.RowsAffected(), err
}

// RecordAuthEventID appends one authentication audit row and returns its id.
func (s *Store) RecordAuthEventID(ctx context.Context, e auth.AuditEvent) (int64, error) {
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
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO auth_audit_events(event_type,user_id,username,remote_addr,created_at,count) VALUES($1,$2::uuid,$3,$4,$5,$6) RETURNING id`, e.Type, user, name, addr, e.At, max(e.Count, 1)).Scan(&id)
	return id, err
}

// CreateLocalSession serializes the verified-hash check and session insertion
// with password changes on the credential row. No hashing runs under the lock.
func (s *Store) CreateLocalSession(ctx context.Context, cred auth.LocalCredential, token, csrf []byte, expiry time.Time, remoteAddr string, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin local session: %w", err)
	}
	defer tx.Rollback(ctx)
	var hash string
	var disabled bool
	err = tx.QueryRow(ctx, `SELECT password_hash,disabled FROM local_credentials WHERE user_id=$1::uuid FOR UPDATE`, cred.UserID).Scan(&hash, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrNoCredential
	}
	if err != nil {
		return fmt.Errorf("lock local credential: %w", err)
	}
	if disabled || hash != cred.PasswordHash {
		return auth.ErrNoCredential
	}
	if _, err := tx.Exec(ctx, `INSERT INTO auth_sessions(token_hash,user_id,csrf_hash,expires_at) VALUES($1,$2,$3,$4)`, token, cred.UserID, csrf, expiry); err != nil {
		return fmt.Errorf("insert local session: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO auth_audit_events(event_type,user_id,username,remote_addr,created_at) SELECT $2,user_id,username,$3,$4 FROM local_credentials WHERE user_id=$1::uuid`, cred.UserID, auth.AuditSignInSuccess, remoteAddr, now); err != nil {
		return fmt.Errorf("audit local sign-in: %w", err)
	}
	return tx.Commit(ctx)
}
