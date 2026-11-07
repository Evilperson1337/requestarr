package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
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
	// The first-sign-in role applies to new users only (D035); the ON CONFLICT
	// branch below leaves an existing user's role alone.
	var stored string
	if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT value FROM app_settings WHERE key=$1),'')`, settingDefaultRole).Scan(&stored); err != nil {
		return auth.User{}, err
	}
	var created bool
	err = tx.QueryRow(ctx, `INSERT INTO users(id,oidc_subject,email,display_name,role,created_at,updated_at)
		VALUES($1,$2,NULLIF($3,''),$4,$6,$5,$5)
		ON CONFLICT (oidc_subject) DO UPDATE SET email=NULLIF(EXCLUDED.email,''),display_name=EXCLUDED.display_name,updated_at=EXCLUDED.updated_at
		RETURNING id::text,(xmax=0)`, uid, identityKey, email, name, now, firstSignInRole(stored)).Scan(&existing, &created)
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
	if created {
		if err = notifyFirstSignIn(ctx, tx, user); err != nil {
			return auth.User{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return auth.User{}, err
	}
	return user, nil
}

// notifyFirstSignIn tells every administrator, in-app, that someone new has
// signed in. One notice per administrator per person; never the person.
func notifyFirstSignIn(ctx context.Context, tx pgx.Tx, user auth.User) error {
	who := user.DisplayName
	if who == "" {
		who = user.Email
	}
	if who == "" {
		who = "A new person"
	}
	title := shorten(who, 200) + " signed in for the first time as " + user.Role
	if _, err := tx.Exec(ctx, `INSERT INTO notification_deliveries(id,event_key,user_id,channel,status,title,body,link,created_at,available_at,updated_at)
		SELECT gen_random_uuid(),'user.first_sign_in:'||$1::text,u.id,'in_app','pending',$2,$3,'/settings?tab=users',now(),now(),now() FROM users u WHERE u.role IN (`+domain.SQLRoleList(domain.RolesThatManageSystem())+`) AND u.id<>$1::uuid
		ON CONFLICT(event_key,user_id,channel) WHERE event_key IS NOT NULL DO NOTHING`, user.ID, title, "Open Settings, Users to change their role."); err != nil {
		return fmt.Errorf("enqueue first sign-in notices: %w", err)
	}
	return nil
}
func (s *Store) CreateSession(ctx context.Context, user string, tokenHash, csrfHash []byte, expiry time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO auth_sessions(token_hash,user_id,csrf_hash,expires_at) VALUES($1,$2,$3,$4)`, tokenHash, user, csrfHash, expiry)
	return err
}
func (s *Store) GetSession(ctx context.Context, hash []byte, now time.Time) (auth.User, []byte, time.Time, error) {
	var u auth.User
	var csrf []byte
	var exp time.Time
	// The built-in local administrator has no OIDC identity, hence the LEFT JOIN. Its
	// must-change flag rides along, and a disabled local account has no live session.
	err := s.pool.QueryRow(ctx, `SELECT u.id::text,u.role,COALESCE(i.email,''),COALESCE(i.display_name,u.display_name),COALESCE(i.issuer,''),COALESCE(i.subject,''),COALESCE(lc.must_change,FALSE),lc.user_id IS NOT NULL,s.csrf_hash,s.expires_at FROM auth_sessions s JOIN users u ON u.id=s.user_id LEFT JOIN oidc_identities i ON i.user_id=u.id LEFT JOIN local_credentials lc ON lc.user_id=u.id WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>$2 AND NOT COALESCE(lc.disabled,FALSE)`, hash, now).Scan(&u.ID, &u.Role, &u.Email, &u.DisplayName, &u.Issuer, &u.Subject, &u.MustChange, &u.LocalAdministrator, &csrf, &exp)
	return u, csrf, exp, err
}
func (s *Store) RevokeSession(ctx context.Context, hash []byte, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE auth_sessions SET revoked_at=$2 WHERE token_hash=$1 AND revoked_at IS NULL`, hash, now)
	return err
}

type AdminUser struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Builtin     bool      `json:"builtin"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (s *Store) ListAdminUsers(ctx context.Context) ([]AdminUser, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id::text, COALESCE(i.email,''), COALESCE(i.display_name,u.display_name,''), u.role, u.builtin, u.created_at FROM users u LEFT JOIN LATERAL (SELECT email,display_name FROM oidc_identities WHERE user_id=u.id ORDER BY issuer,subject LIMIT 1) i ON true ORDER BY u.builtin DESC, lower(COALESCE(i.display_name,'')), lower(COALESCE(i.email,'')), u.id`)
	if err != nil {
		return nil, fmt.Errorf("list admin users: %w", err)
	}
	defer rows.Close()
	users := make([]AdminUser, 0)
	for rows.Next() {
		var user AdminUser
		if err := rows.Scan(&user.ID, &user.Email, &user.DisplayName, &user.Role, &user.Builtin, &user.CreatedAt); err != nil {
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
var ErrLastSuperuser = errors.New("cannot remove the last superuser who can sign in")
var ErrAdminUserNotFound = errors.New("user not found")

func (s *Store) ChangeUserRole(ctx context.Context, actorID, targetID, role string, now time.Time) error {
	if !domain.Role(role).Stored() {
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
	// The actor's role is read here, under the lock, so a refusal never rests on a stale session.
	var actorRole string
	err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, actorID).Scan(&actorRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return &domain.RoleChangeRefused{Needs: domain.RoleAdmin, Reason: "changing a person's role is for administrators"}
	}
	if err != nil {
		return fmt.Errorf("read actor role: %w", err)
	}
	var previous string
	var builtin bool
	err = tx.QueryRow(ctx, `SELECT role,builtin FROM users WHERE id=$1 FOR UPDATE`, targetID).Scan(&previous, &builtin)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAdminUserNotFound
	}
	if err != nil {
		return fmt.Errorf("read target role: %w", err)
	}
	if builtin && role != string(domain.RoleSuperuser) {
		return auth.ErrBuiltinRole
	}
	if err = domain.Role(actorRole).CheckRoleChange(domain.Role(previous), domain.Role(role)); err != nil {
		return err
	}
	if previous == role {
		return tx.Commit(ctx)
	}
	if domain.Role(previous).CanManageAccess() && !domain.Role(role).CanManageAccess() {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM users u WHERE role IN (`+domain.SQLRoleList(domain.RolesThatManageAccess())+`) AND NOT EXISTS(SELECT 1 FROM local_credentials c WHERE c.user_id=u.id AND c.disabled)`).Scan(&count); err != nil {
			return fmt.Errorf("count superusers: %w", err)
		}
		if count <= 1 {
			return ErrLastSuperuser
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET role=$2,updated_at=$3 WHERE id=$1`, targetID, role, now); err != nil {
		return fmt.Errorf("update user role: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, targetID, now); err != nil {
		return fmt.Errorf("revoke target sessions: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_user_audit_events(actor_user_id,target_user_id,previous_role,new_role,source,created_at) VALUES($1,$2,$3,$4,'manual',$5)`, actorID, targetID, previous, role, now); err != nil {
		return fmt.Errorf("audit role change: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit role change: %w", err)
	}
	return nil
}
