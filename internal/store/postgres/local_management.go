package postgres

import (
	"context"
	"errors"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ auth.AdminManagementRepository = (*Store)(nil)

func (s *Store) LocalAdminStatus(ctx context.Context) (auth.LocalAdminStatus, error) {
	var st auth.LocalAdminStatus
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_credentials),COALESCE((SELECT disabled FROM local_credentials WHERE username='admin'),false),EXISTS(SELECT 1 FROM users WHERE role IN (`+domain.SQLRoleList(domain.RolesThatManageAccess())+`) AND NOT builtin)`).Scan(&st.Exists, &st.Disabled, &st.OtherAdmin)
	st.CanDisable = st.Exists && !st.Disabled && st.OtherAdmin
	return st, err
}
func (s *Store) CreateRecoveryAdmin(ctx context.Context, actor, hash string, now time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, adminRoleAdvisoryLockID); err != nil {
		return false, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE builtin)`).Scan(&exists); err != nil || exists {
		return false, err
	}
	id := newUUID()
	if _, err = tx.Exec(ctx, `INSERT INTO users(id,oidc_subject,display_name,role,builtin,created_at,updated_at) VALUES($1,$2,'Administrator',$4,true,$3,$3)`, id, localAdminSubject, now, domain.RoleSuperuser); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO local_credentials(user_id,username,password_hash,must_change,created_at,updated_at) VALUES($1,'admin',$2,true,$3,$3)`, id, hash, now); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_audit_events(event_type,user_id,username,created_at) VALUES('admin_created',$1,'admin',$2)`, id, now); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO local_admin_management_audit_events(actor_user_id,target_user_id,action,created_at) VALUES($1,$2,'create',$3)`, actor, id, now); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
func (s *Store) SetLocalAdminDisabled(ctx context.Context, actor string, disabled bool, now time.Time, guard func() bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, adminRoleAdvisoryLockID); err != nil {
		return err
	}
	var id string
	var old bool
	err = tx.QueryRow(ctx, `SELECT user_id::text,disabled FROM local_credentials WHERE username='admin' FOR UPDATE`).Scan(&id, &old)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrNoCredential
	}
	if err != nil {
		return err
	}
	if old == disabled {
		return nil
	}
	if disabled {
		var another bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE role IN (`+domain.SQLRoleList(domain.RolesThatManageAccess())+`) AND NOT builtin)`).Scan(&another); err != nil {
			return err
		}
		if !another {
			return auth.ErrLastLocalAdmin
		}
		// Evaluated while the lock is held, so the answer cannot go stale
		// between the check and the update.
		if guard != nil && !guard() {
			return auth.ErrNoWorkingSignIn
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE local_credentials SET disabled=$1,updated_at=$2 WHERE user_id=$3`, disabled, now, id); err != nil {
		return err
	}
	if disabled {
		if _, err = tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, id, now); err != nil {
			return err
		}
	}
	action := "enable"
	if disabled {
		action = "disable"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO local_admin_management_audit_events(actor_user_id,target_user_id,action,created_at) VALUES($1,$2,$3,$4)`, actor, id, action, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
