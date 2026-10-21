package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/example/requestarr/internal/auth"
	"github.com/jackc/pgx/v5"
)

const adminRoleAdvisoryLockID = 783214091

func (s *Store) SaveTransaction(ctx context.Context, hash, nonce, verifier, preauth string, expiry time.Time) error {
	decoded, err := hex.DecodeString(hash)
	if err != nil {
		return fmt.Errorf("decode state hash: %w", err)
	}
	preauthBytes, err := hex.DecodeString(preauth)
	if err != nil {
		return fmt.Errorf("decode pre-auth hash: %w", err)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO oidc_transactions(state_hash,nonce,pkce_verifier,preauth_hash,expires_at) VALUES($1,$2,$3,$4,$5)`, decoded, nonce, verifier, preauthBytes, expiry)
	return err
}
func (s *Store) ConsumeTransaction(ctx context.Context, hash, preauth string, now time.Time) (string, string, error) {
	decoded, err := hex.DecodeString(hash)
	if err != nil {
		return "", "", fmt.Errorf("decode state hash: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	var nonce, verifier string
	preauthBytes, err := hex.DecodeString(preauth)
	if err != nil {
		return "", "", fmt.Errorf("decode pre-auth hash: %w", err)
	}
	err = tx.QueryRow(ctx, `DELETE FROM oidc_transactions WHERE state_hash=$1 AND preauth_hash=$2 AND expires_at>$3 RETURNING nonce,pkce_verifier`, decoded, preauthBytes, now).Scan(&nonce, &verifier)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", errors.New("OIDC transaction missing or expired")
		}
		return "", "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", err
	}
	return nonce, verifier, nil
}

var _ auth.Repository = (*Store)(nil)

func (s *Store) Login(ctx context.Context, issuer, subject, email, name, uid string, now time.Time) (auth.User, error) {
	if issuer == "" || subject == "" {
		return auth.User{}, fmt.Errorf("OIDC issuer and subject are required")
	}
	if uid == "" {
		return auth.User{}, fmt.Errorf("user ID is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return auth.User{}, err
	}
	defer tx.Rollback(ctx)
	var user auth.User
	var existing string
	// Keep the legacy users.oidc_subject key printable. The exact issuer/subject
	// pair is stored separately in oidc_identities, whose composite PK is authoritative.
	identity := make([]byte, 0, len(issuer)+1+len(subject))
	identity = append(identity, issuer...)
	identity = append(identity, 0)
	identity = append(identity, subject...)
	digest := sha256.Sum256(identity)
	identityKey := "oidc:" + hex.EncodeToString(digest[:])
	err = tx.QueryRow(ctx, `INSERT INTO users(id,oidc_subject,email,display_name,role,created_at,updated_at)
		VALUES($1,$2,NULLIF($3,''),$4,'viewer',$5,$5)
		ON CONFLICT (oidc_subject) DO UPDATE SET email=NULLIF(EXCLUDED.email,''),display_name=EXCLUDED.display_name,updated_at=EXCLUDED.updated_at
		RETURNING id::text`, uid, identityKey, email, name, now).Scan(&existing)
	if err == nil {
		err = tx.QueryRow(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,email,display_name)
			VALUES($1,$2,$3,$4,$5)
			ON CONFLICT (issuer,subject) DO UPDATE SET email=EXCLUDED.email,display_name=EXCLUDED.display_name
			RETURNING user_id::text`, issuer, subject, existing, email, name).Scan(&existing)
	}
	if err != nil {
		return auth.User{}, err
	}
	err = tx.QueryRow(ctx, `SELECT u.id::text,u.role,i.email,i.display_name,i.issuer,i.subject FROM users u JOIN oidc_identities i ON i.user_id=u.id WHERE u.id=$1`, existing).Scan(&user.ID, &user.Role, &user.Email, &user.DisplayName, &user.Issuer, &user.Subject)
	if err != nil {
		return auth.User{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return auth.User{}, err
	}
	return user, nil
}
func (s *Store) CreateSession(ctx context.Context, user string, tokenHash, csrfHash []byte, expiry time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO auth_sessions(token_hash,user_id,csrf_hash,expires_at) VALUES($1,$2,$3,$4)`, tokenHash, user, csrfHash, expiry)
	return err
}
func (s *Store) GetSession(ctx context.Context, hash []byte, now time.Time) (auth.User, []byte, time.Time, error) {
	var u auth.User
	var csrf []byte
	var exp time.Time
	err := s.pool.QueryRow(ctx, `SELECT u.id::text,u.role,i.email,i.display_name,i.issuer,i.subject,s.csrf_hash,s.expires_at FROM auth_sessions s JOIN users u ON u.id=s.user_id JOIN oidc_identities i ON i.user_id=u.id WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>$2`, hash, now).Scan(&u.ID, &u.Role, &u.Email, &u.DisplayName, &u.Issuer, &u.Subject, &csrf, &exp)
	return u, csrf, exp, err
}
func (s *Store) RevokeSession(ctx context.Context, hash []byte, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE auth_sessions SET revoked_at=$2 WHERE token_hash=$1 AND revoked_at IS NULL`, hash, now)
	return err
}

func (s *Store) IssueAdminToken(ctx context.Context, issuer, subject, token string, expires time.Time, rotate bool) error {
	if issuer == "" || subject == "" || token == "" || len(issuer) > 2048 || len(subject) > 1024 {
		return fmt.Errorf("invalid bootstrap target")
	}
	hash := sha256.Sum256([]byte(token))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var sql string
	if rotate {
		sql = `UPDATE setup_state SET target_issuer=$1,target_subject=$2,token_hash=$3,token_expires_at=$4 WHERE singleton=TRUE AND complete=FALSE AND (token_hash IS NULL OR token_expires_at<=now())`
	} else {
		sql = `UPDATE setup_state SET target_issuer=$1,target_subject=$2,token_hash=$3,token_expires_at=$4 WHERE singleton=TRUE AND complete=FALSE AND token_hash IS NULL`
	}
	tag, err := tx.Exec(ctx, sql, issuer, subject, hash[:], expires)
	if err != nil {
		return fmt.Errorf("issue admin bootstrap token: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("bootstrap unavailable")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO setup_audit_events(id,event_type,target_issuer,target_subject) VALUES($1,'admin_token_issued',$2,$3)`, newUUID(), issuer, subject); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ClaimAdmin(ctx context.Context, userID, issuer, subject string, hash []byte, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, adminRoleAdvisoryLockID); err != nil {
		return fmt.Errorf("lock admin claim: %w", err)
	}
	var targetIssuer, targetSubject string
	err = tx.QueryRow(ctx, `UPDATE setup_state SET complete=TRUE,claimed_by=$1,completed_at=$2,token_hash=NULL,token_expires_at=NULL WHERE singleton=TRUE AND complete=FALSE AND target_issuer=$3 AND target_subject=$4 AND token_hash=$5 AND token_expires_at>$2 RETURNING target_issuer,target_subject`, userID, now, issuer, subject, hash).Scan(&targetIssuer, &targetSubject)
	if err != nil {
		return fmt.Errorf("claim unavailable")
	}
	result, err := tx.Exec(ctx, `UPDATE users SET role='admin',updated_at=$2 WHERE id=$1 AND EXISTS (SELECT 1 FROM oidc_identities WHERE user_id=users.id AND issuer=$3 AND subject=$4)`, userID, now, issuer, subject)
	if err != nil || result.RowsAffected() != 1 {
		return fmt.Errorf("claim unavailable")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO setup_audit_events(id,event_type,actor_user_id,target_issuer,target_subject,created_at) VALUES($1,'admin_claimed',$2,$3,$4,$5)`, newUUID(), userID, issuer, subject, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SetupComplete(ctx context.Context) (bool, error) {
	var complete bool
	err := s.pool.QueryRow(ctx, `SELECT complete FROM setup_state WHERE singleton=TRUE`).Scan(&complete)
	return complete, err
}

type AdminUser struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (s *Store) ListAdminUsers(ctx context.Context) ([]AdminUser, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id::text, COALESCE(i.email,''), COALESCE(i.display_name,''), u.role, u.created_at FROM users u LEFT JOIN LATERAL (SELECT email,display_name FROM oidc_identities WHERE user_id=u.id ORDER BY issuer,subject LIMIT 1) i ON true ORDER BY lower(COALESCE(i.display_name,'')), lower(COALESCE(i.email,'')), u.id`)
	if err != nil {
		return nil, fmt.Errorf("list admin users: %w", err)
	}
	defer rows.Close()
	users := make([]AdminUser, 0)
	for rows.Next() {
		var user AdminUser
		if err := rows.Scan(&user.ID, &user.Email, &user.DisplayName, &user.Role, &user.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan admin user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin users: %w", err)
	}
	return users, nil
}

var ErrAdminSelfRoleChange = errors.New("cannot change your own role")
var ErrLastAdmin = errors.New("cannot remove the last administrator")
var ErrAdminUserNotFound = errors.New("user not found")

func (s *Store) ChangeUserRole(ctx context.Context, actorID, targetID, role string, now time.Time) error {
	if role != "admin" && role != "requester" {
		return fmt.Errorf("invalid role")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin role change: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, adminRoleAdvisoryLockID); err != nil {
		return fmt.Errorf("lock role changes: %w", err)
	}
	if actorID == targetID {
		return ErrAdminSelfRoleChange
	}
	var previous string
	err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR UPDATE`, targetID).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAdminUserNotFound
	}
	if err != nil {
		return fmt.Errorf("read target role: %w", err)
	}
	if previous == role {
		return tx.Commit(ctx)
	}
	if previous == "admin" && role == "requester" {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin'`).Scan(&count); err != nil {
			return fmt.Errorf("count administrators: %w", err)
		}
		if count <= 1 {
			return ErrLastAdmin
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET role=$2,updated_at=$3 WHERE id=$1`, targetID, role, now); err != nil {
		return fmt.Errorf("update user role: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, targetID, now); err != nil {
		return fmt.Errorf("revoke target sessions: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_user_audit_events(actor_user_id,target_user_id,previous_role,new_role,created_at) VALUES($1,$2,$3,$4,$5)`, actorID, targetID, previous, role, now); err != nil {
		return fmt.Errorf("audit role change: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit role change: %w", err)
	}
	return nil
}
