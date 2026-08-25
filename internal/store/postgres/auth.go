package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
	"git.example.com/evilperson1337/Requestarr/internal/secrets"
	"github.com/jackc/pgx/v5"
)

// AuthRepository reserves the concrete PostgreSQL boundary for normal auth.
// Query behavior is intentionally deferred until the OIDC callback/session
// service increment; no runtime component constructs this repository yet.
type AuthRepository struct {
	pool *Pool
}

func (r *AuthRepository) CreateLoginTransaction(ctx context.Context, transaction auth.LoginTransaction) error {
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return auth.ErrLoginUnavailable
	}
	_, err := r.pool.pool.Exec(ctx, `INSERT INTO auth_login_transactions(id,state_digest,nonce_digest,pkce_verifier_digest,authentication_revision,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, transaction.ID, transaction.StateDigest[:], transaction.NonceDigest[:], transaction.PKCEVerifierDigest[:], transaction.AuthenticationRevision, transaction.CreatedAt, transaction.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create login transaction: %w", err)
	}
	return nil
}

func (r *AuthRepository) ConsumeLoginTransaction(ctx context.Context, digest [32]byte, _ time.Time) (auth.LoginTransaction, error) {
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return auth.LoginTransaction{}, auth.ErrLoginInvalid
	}
	var transaction auth.LoginTransaction
	var state, nonce, verifier []byte
	err := r.pool.pool.QueryRow(ctx, `UPDATE auth_login_transactions SET consumed_at=clock_timestamp() WHERE state_digest=$1 AND consumed_at IS NULL AND expires_at>clock_timestamp() RETURNING id,state_digest,nonce_digest,pkce_verifier_digest,authentication_revision,created_at,expires_at`, digest[:]).Scan(&transaction.ID, &state, &nonce, &verifier, &transaction.AuthenticationRevision, &transaction.CreatedAt, &transaction.ExpiresAt)
	if err != nil || len(state) != 32 || len(nonce) != 32 || len(verifier) != 32 {
		return auth.LoginTransaction{}, auth.ErrLoginInvalid
	}
	copy(transaction.StateDigest[:], state)
	copy(transaction.NonceDigest[:], nonce)
	copy(transaction.PKCEVerifierDigest[:], verifier)
	return transaction, nil
}

func (r *AuthRepository) CreateSession(ctx context.Context, session auth.Session) error {
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return auth.ErrLoginUnavailable
	}
	_, err := r.pool.pool.Exec(ctx, `INSERT INTO auth_sessions(id,token_digest,user_id,authentication_revision,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, session.ID, session.TokenDigest[:], session.UserID, session.AuthenticationRevision, session.CreatedAt, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create auth session: %w", err)
	}
	return nil
}

func (r *AuthRepository) ReadSession(ctx context.Context, digest [32]byte, _ time.Time) (auth.Session, error) {
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return auth.Session{}, auth.ErrSessionRequired
	}
	var session auth.Session
	var token []byte
	err := r.pool.pool.QueryRow(ctx, `SELECT id,token_digest,user_id,authentication_revision,created_at,expires_at,revoked_at FROM auth_sessions WHERE token_digest=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp()`, digest[:]).Scan(&session.ID, &token, &session.UserID, &session.AuthenticationRevision, &session.CreatedAt, &session.ExpiresAt, &session.RevokedAt)
	if err != nil || len(token) != 32 {
		return auth.Session{}, auth.ErrSessionRequired
	}
	copy(session.TokenDigest[:], token)
	return session, nil
}

func (r *AuthRepository) ReadUser(ctx context.Context, id string) (auth.User, error) {
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return auth.User{}, auth.ErrSessionRequired
	}
	var user auth.User
	err := r.pool.pool.QueryRow(ctx, `SELECT id,oidc_issuer,oidc_subject,email,display_name,role FROM users WHERE id=$1`, id).Scan(&user.ID, &user.Issuer, &user.Subject, &user.Email, &user.DisplayName, &user.Role)
	if err != nil {
		return auth.User{}, auth.ErrSessionRequired
	}
	return user, nil
}

func (r *AuthRepository) RevokeSession(ctx context.Context, id string, _ time.Time) error {
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return auth.ErrSessionRequired
	}
	_, err := r.pool.pool.Exec(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE id=$1`, id)
	if err != nil {
		return auth.ErrSessionRequired
	}
	return nil
}

func (r *AuthRepository) UpsertUser(ctx context.Context, user auth.User, now time.Time) (auth.User, error) {
	if r == nil || r.pool == nil || r.pool.pool == nil || user.Issuer == "" || user.Subject == "" || user.DisplayName == "" || !user.Role.Valid() {
		return auth.User{}, auth.ErrLoginInvalid
	}
	var saved auth.User
	err := r.pool.pool.QueryRow(ctx, `INSERT INTO users(id,oidc_issuer,oidc_subject,email,display_name,role,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7) ON CONFLICT(oidc_issuer,oidc_subject) DO UPDATE SET email=EXCLUDED.email,display_name=EXCLUDED.display_name,role=EXCLUDED.role,updated_at=EXCLUDED.updated_at RETURNING id,oidc_issuer,oidc_subject,email,display_name,role`, user.ID, user.Issuer, user.Subject, user.Email, user.DisplayName, user.Role, now).Scan(&saved.ID, &saved.Issuer, &saved.Subject, &saved.Email, &saved.DisplayName, &saved.Role)
	if err != nil {
		return auth.User{}, fmt.Errorf("upsert OIDC user: %w", err)
	}
	return saved, nil
}

func (r *AuthRepository) CreateAuditEvent(ctx context.Context, event auth.AuditEvent) error {
	if r == nil || r.pool == nil || r.pool.pool == nil || event.ID == "" {
		return auth.ErrLoginUnavailable
	}
	_, err := r.pool.pool.Exec(ctx, `INSERT INTO auth_audit_events(id,event_type,user_id,session_id,created_at) VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5)`, event.ID, event.EventType, event.UserID, event.SessionID, event.CreatedAt)
	return err
}

// NewAuthRepository constructs the normal-auth persistence adapter.
func NewAuthRepository(pool *Pool) *AuthRepository {
	return &AuthRepository{pool: pool}
}

// ReadRuntimeConfig reads the activated singleton through a typed, bounded
// representation. JSON and semantic validation prevent malformed persistence
// data from becoming runtime authentication behavior.
func (r *AuthRepository) ReadRuntimeConfig(ctx context.Context) (auth.RuntimeConfig, error) {
	if err := ctx.Err(); err != nil {
		return auth.RuntimeConfig{}, auth.ErrRuntimeConfigUnavailable
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return auth.RuntimeConfig{}, auth.ErrRuntimeConfigUnavailable
	}
	var config auth.RuntimeConfig
	var scopes, mappings []byte
	err := r.pool.pool.QueryRow(ctx, `SELECT authentication_revision,issuer,client_id,client_type,scopes,groups_claim,default_role,group_role_mappings,activated_at
		FROM auth_runtime_config WHERE singleton=TRUE`).Scan(&config.AuthenticationRevision, &config.Issuer, &config.ClientID, &config.ClientType, &scopes, &config.GroupsClaim, &config.DefaultRole, &mappings, &config.ActivatedAt)
	if err != nil {
		return auth.RuntimeConfig{}, runtimeConfigError(ctx, err)
	}
	if len(scopes) > 2048 || len(mappings) > 16384 || json.Unmarshal(scopes, &config.Scopes) != nil {
		return auth.RuntimeConfig{}, auth.ErrRuntimeConfigInvalid
	}
	var storedMappings []struct {
		Group string `json:"group"`
		Role  string `json:"role"`
	}
	if json.Unmarshal(mappings, &storedMappings) != nil {
		return auth.RuntimeConfig{}, auth.ErrRuntimeConfigInvalid
	}
	config.GroupRoles = make([]auth.GroupRoleMapping, len(storedMappings))
	for i, mapping := range storedMappings {
		config.GroupRoles[i] = auth.GroupRoleMapping{Group: mapping.Group, Role: auth.Role(mapping.Role)}
	}
	if err := config.Validate(); err != nil {
		return auth.RuntimeConfig{}, err
	}
	return config, nil
}

func runtimeConfigError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return auth.ErrRuntimeConfigUnavailable
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrRuntimeConfigNotActivated
	}
	return fmt.Errorf("read runtime authentication configuration: %w", auth.ErrRuntimeConfigUnavailable)
}

// RuntimeSecretReader decrypts only the activated runtime-secret source. It
// never consults setup draft secret storage and clears plaintext after use.
type RuntimeSecretReader struct {
	secrets *EncryptedSecretRepository
	cipher  *secrets.Cipher
}

func NewRuntimeSecretReader(repository *EncryptedSecretRepository, cipher *secrets.Cipher) *RuntimeSecretReader {
	return &RuntimeSecretReader{secrets: repository, cipher: cipher}
}

func (r *RuntimeSecretReader) UseRuntimeClientSecret(ctx context.Context, use func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return auth.ErrRuntimeSecretUnavailable
	}
	if r == nil || r.secrets == nil || r.cipher == nil || use == nil {
		return auth.ErrRuntimeSecretUnavailable
	}
	envelope, err := r.secrets.Read(ctx, runtimeAuthenticationSecretContext)
	if err != nil {
		return auth.ErrRuntimeSecretUnavailable
	}
	plaintext, err := r.cipher.Decrypt(runtimeAuthenticationSecretContext, envelope)
	if err != nil {
		return auth.ErrRuntimeSecretUnavailable
	}
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()
	if err := use(plaintext); err != nil {
		return err
	}
	return nil
}
