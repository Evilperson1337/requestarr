package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/jackc/pgx/v5"
)

const settingOIDC = "authentication.oidc"

var _ authsettings.Repository = (*Store)(nil)

// LoadOIDC uses one statement so metadata and encrypted secret share a snapshot.
func (s *Store) LoadOIDC(ctx context.Context) (*authsettings.Record, error) {
	var data string
	var ciphertext []byte
	var keyID *string
	err := s.pool.QueryRow(ctx, `SELECT a.value,b.ciphertext,b.key_id FROM app_settings a LEFT JOIN stored_secrets b ON b.name=$2 WHERE a.key=$1`, settingOIDC, string(secrets.OIDCClientSecret)).Scan(&data, &ciphertext, &keyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, authsettings.ErrStorage
	}
	r := &authsettings.Record{}
	if json.Unmarshal([]byte(data), &r.Settings) != nil {
		return nil, authsettings.ErrStorage
	}
	if keyID != nil {
		r.Secret = &secrets.Stored{Name: secrets.OIDCClientSecret, Sealed: secrets.Sealed{Ciphertext: ciphertext, KeyID: *keyID}}
	}
	return r, nil
}

// SaveOIDC commits metadata, encrypted secret and value-free audit together.
func (s *Store) SaveOIDC(ctx context.Context, actor string, r *authsettings.Record) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return authsettings.ErrStorage
	}
	defer tx.Rollback(ctx)
	// Serialise with other OIDC writes, including discard; only this process
	// publishes runtime snapshots, but the durable operation is indivisible too.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(4036001)`); err != nil {
		return authsettings.ErrStorage
	}
	action := "save"
	if r == nil {
		action = "discard"
		if _, err = tx.Exec(ctx, `DELETE FROM app_settings WHERE key=$1`, settingOIDC); err != nil {
			return authsettings.ErrStorage
		}
		tag, e := tx.Exec(ctx, `DELETE FROM stored_secrets WHERE name=$1`, string(secrets.OIDCClientSecret))
		if e != nil {
			return authsettings.ErrStorage
		}
		if tag.RowsAffected() > 0 {
			if _, err = tx.Exec(ctx, `INSERT INTO secret_audit_events(actor_user_id,secret_name,action) VALUES($1,$2,'clear')`, actor, string(secrets.OIDCClientSecret)); err != nil {
				return authsettings.ErrStorage
			}
		}
	} else {
		data, e := json.Marshal(r.Settings)
		if e != nil {
			return authsettings.ErrStorage
		}
		if _, err = tx.Exec(ctx, `INSERT INTO app_settings(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=now()`, settingOIDC, string(data)); err != nil {
			return authsettings.ErrStorage
		}
		var oldCipher []byte
		var oldKey string
		err = tx.QueryRow(ctx, `SELECT ciphertext,key_id FROM stored_secrets WHERE name=$1`, string(secrets.OIDCClientSecret)).Scan(&oldCipher, &oldKey)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return authsettings.ErrStorage
		}
		if r.Secret != nil {
			if oldKey != r.Secret.KeyID || !bytes.Equal(oldCipher, r.Secret.Ciphertext) {
				if putStoredSecretTx(ctx, tx, actor, secrets.OIDCClientSecret, r.Secret.Sealed) != nil {
					return authsettings.ErrStorage
				}
			}
		} else if oldKey != "" {
			return authsettings.ErrStorage
		} // metadata must not orphan a previously saved secret

	}
	if _, err = tx.Exec(ctx, `INSERT INTO authentication_settings_audit_events(actor_user_id,action) VALUES($1,$2)`, actor, action); err != nil {
		return authsettings.ErrStorage
	}
	if tx.Commit(ctx) != nil {
		return authsettings.ErrStorage
	}
	return nil
}
