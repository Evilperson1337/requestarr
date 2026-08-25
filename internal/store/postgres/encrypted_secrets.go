package postgres

import (
	"bytes"
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"git.example.com/evilperson1337/Requestarr/internal/secrets"
)

var (
	ErrEncryptedSecretNotFound    = errors.New("encrypted secret not found")
	ErrEncryptedSecretInvalid     = errors.New("encrypted secret record invalid")
	ErrEncryptedSecretCanceled    = errors.New("encrypted secret operation canceled")
	ErrEncryptedSecretUnavailable = errors.New("encrypted secret storage unavailable")
	ErrEncryptionKeyRequired      = errors.New("encrypted secret key required")
	ErrEncryptionKeyIncompatible  = errors.New("encrypted secret key incompatible")
)

// EncryptedSecretExecutor is the narrow database surface needed to participate
// in either pool-owned statements or a caller-owned transaction.
type EncryptedSecretExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// EncryptedSecretRepository persists opaque envelopes; encryption policy and
// plaintext never cross this adapter. The active identifier is immutable.
type EncryptedSecretRepository struct {
	pool      *Pool
	activeKey *secrets.KeyID
}

func NewEncryptedSecretRepository(pool *Pool, activeKey *secrets.KeyID) *EncryptedSecretRepository {
	r := &EncryptedSecretRepository{pool: pool}
	if activeKey != nil {
		keyID := *activeKey
		r.activeKey = &keyID
	}
	return r
}

func (r *EncryptedSecretRepository) Read(ctx context.Context, semantic secrets.Context) (secrets.Envelope, error) {
	if err := ctx.Err(); err != nil {
		return secrets.Envelope{}, ErrEncryptedSecretCanceled
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return secrets.Envelope{}, ErrEncryptedSecretUnavailable
	}
	return r.read(ctx, r.pool.pool, semantic)
}

// ReadTx reads an opaque envelope through a caller-owned transaction.
func (r *EncryptedSecretRepository) ReadTx(ctx context.Context, tx pgx.Tx, semantic secrets.Context) (secrets.Envelope, error) {
	if tx == nil {
		return secrets.Envelope{}, ErrEncryptedSecretUnavailable
	}
	return r.read(ctx, tx, semantic)
}

func (r *EncryptedSecretRepository) read(ctx context.Context, executor EncryptedSecretExecutor, semantic secrets.Context) (secrets.Envelope, error) {
	if err := ctx.Err(); err != nil {
		return secrets.Envelope{}, ErrEncryptedSecretCanceled
	}
	var version int16
	var keyID, nonce, ciphertext, identity []byte
	err := executor.QueryRow(ctx, `SELECT envelope_version,key_id,nonce,ciphertext,credential_identity FROM encrypted_secrets
		WHERE namespace=$1 AND owner_id=$2 AND secret_name=$3`, semantic.Namespace, semantic.OwnerID, semantic.Name).
		Scan(&version, &keyID, &nonce, &ciphertext, &identity)
	if errors.Is(err, pgx.ErrNoRows) {
		return secrets.Envelope{}, ErrEncryptedSecretNotFound
	}
	if err != nil {
		return secrets.Envelope{}, classifyEncryptedSecretError(ctx, err)
	}
	if version != int16(secrets.EnvelopeVersion) || len(keyID) != secrets.KeyIDSize || len(nonce) != secrets.NonceSize || len(ciphertext) < 16 || len(ciphertext) > secrets.MaxCiphertext || len(identity) != 32 {
		return secrets.Envelope{}, ErrEncryptedSecretInvalid
	}
	var envelope secrets.Envelope
	envelope.Version = uint8(version)
	copy(envelope.KeyID[:], keyID)
	copy(envelope.Nonce[:], nonce)
	envelope.Ciphertext = append([]byte(nil), ciphertext...)
	copy(envelope.Identity[:], identity)
	return envelope, nil
}

// DeleteTx removes an envelope in a caller-owned transaction. Absence is an
// idempotent success.
func (r *EncryptedSecretRepository) DeleteTx(ctx context.Context, tx pgx.Tx, semantic secrets.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, ErrEncryptedSecretCanceled
	}
	if tx == nil {
		return false, ErrEncryptedSecretUnavailable
	}
	tag, err := tx.Exec(ctx, `DELETE FROM encrypted_secrets WHERE namespace=$1 AND owner_id=$2 AND secret_name=$3`,
		semantic.Namespace, semantic.OwnerID[:], semantic.Name)
	if err != nil {
		return false, classifyEncryptedSecretError(ctx, err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *EncryptedSecretRepository) Upsert(ctx context.Context, semantic secrets.Context, envelope secrets.Envelope) error {
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return ErrEncryptedSecretUnavailable
	}
	return r.upsert(ctx, r.pool.pool, semantic, envelope)
}

// UpsertTx writes through a caller-owned transaction so a future setup save can
// commit its state and encrypted envelope atomically. The caller owns commit or
// rollback; this method never changes transaction lifecycle.
func (r *EncryptedSecretRepository) UpsertTx(ctx context.Context, tx pgx.Tx, semantic secrets.Context, envelope secrets.Envelope) error {
	if tx == nil {
		return ErrEncryptedSecretUnavailable
	}
	return r.upsert(ctx, tx, semantic, envelope)
}

func (r *EncryptedSecretRepository) upsert(ctx context.Context, executor EncryptedSecretExecutor, semantic secrets.Context, envelope secrets.Envelope) error {
	if err := ctx.Err(); err != nil {
		return ErrEncryptedSecretCanceled
	}
	if r == nil || r.activeKey == nil {
		return ErrEncryptionKeyRequired
	}
	if semantic.Namespace == "" || len(semantic.Namespace) > secrets.MaxNamespace || semantic.Name == "" || len(semantic.Name) > secrets.MaxSecretName || semantic.OwnerID == (secrets.OwnerID{}) || envelope.Version != secrets.EnvelopeVersion || envelope.KeyID != *r.activeKey || len(envelope.Ciphertext) < 16 || len(envelope.Ciphertext) > secrets.MaxCiphertext {
		return ErrEncryptedSecretInvalid
	}
	tag, err := executor.Exec(ctx, `INSERT INTO encrypted_secrets
		(namespace,owner_id,secret_name,envelope_version,key_id,nonce,ciphertext,credential_identity,created_at,updated_at)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8,transaction_timestamp(),transaction_timestamp()
		FROM encryption_key_registry WHERE singleton=TRUE AND active_key_id=$5
		ON CONFLICT (namespace,owner_id,secret_name) DO UPDATE SET
		envelope_version=EXCLUDED.envelope_version,key_id=EXCLUDED.key_id,nonce=EXCLUDED.nonce,
		ciphertext=EXCLUDED.ciphertext,credential_identity=EXCLUDED.credential_identity,updated_at=transaction_timestamp()`, semantic.Namespace, semantic.OwnerID[:], semantic.Name,
		envelope.Version, envelope.KeyID[:], envelope.Nonce[:], envelope.Ciphertext, envelope.Identity[:])
	if err != nil {
		return classifyEncryptedSecretError(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return ErrEncryptionKeyIncompatible
	}
	return nil
}

// EstablishActiveKey atomically registers the first usable key or verifies the
// durable singleton. It also rejects inconsistent legacy state in the same
// transaction, before any writer or listener can proceed.
func (r *EncryptedSecretRepository) EstablishActiveKey(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return ErrEncryptedSecretCanceled
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return ErrEncryptedSecretUnavailable
	}
	tx, err := r.pool.pool.Begin(ctx)
	if err != nil {
		return classifyEncryptedSecretError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if r.activeKey != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO encryption_key_registry(singleton,active_key_id)
			VALUES (TRUE,$1) ON CONFLICT (singleton) DO NOTHING`, r.activeKey[:]); err != nil {
			return classifyEncryptedSecretError(ctx, err)
		}
	}
	var registered []byte
	var rowCount int64
	err = tx.QueryRow(ctx, `SELECT
		(SELECT active_key_id FROM encryption_key_registry WHERE singleton=TRUE),
		(SELECT count(*) FROM encrypted_secrets)`).Scan(&registered, &rowCount)
	if err != nil {
		return classifyEncryptedSecretError(ctx, err)
	}
	if len(registered) == 0 && rowCount != 0 {
		return ErrEncryptedSecretInvalid
	}
	if len(registered) != 0 && len(registered) != secrets.KeyIDSize {
		return ErrEncryptedSecretInvalid
	}
	if r.activeKey == nil && len(registered) != 0 {
		return ErrEncryptionKeyRequired
	}
	if r.activeKey != nil && !bytes.Equal(registered, r.activeKey[:]) {
		return ErrEncryptionKeyIncompatible
	}
	var incompatible bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(bool_or(key_id <> $1),FALSE) FROM encrypted_secrets`, registered).Scan(&incompatible); err != nil {
		return classifyEncryptedSecretError(ctx, err)
	}
	if incompatible {
		return ErrEncryptionKeyIncompatible
	}
	if err := tx.Commit(ctx); err != nil {
		return classifyEncryptedSecretError(ctx, err)
	}
	return nil
}

func classifyEncryptedSecretError(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrEncryptedSecretCanceled
	}
	return ErrEncryptedSecretUnavailable
}
