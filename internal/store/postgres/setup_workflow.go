package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
	"git.example.com/evilperson1337/Requestarr/internal/auth/oidc"
	"git.example.com/evilperson1337/Requestarr/internal/secrets"
	"git.example.com/evilperson1337/Requestarr/internal/setup"
)

type SetupWorkflowRepository struct {
	pool    *Pool
	secrets *EncryptedSecretRepository
	cipher  *secrets.Cipher
}

func NewSetupWorkflowRepository(pool *Pool, secretRepository *EncryptedSecretRepository, cipher *secrets.Cipher) *SetupWorkflowRepository {
	return &SetupWorkflowRepository{pool: pool, secrets: secretRepository, cipher: cipher}
}

var setupAuthenticationSecretContext = secrets.Context{
	Namespace: "setup.authentication",
	OwnerID:   secrets.OwnerID{15: 1},
	Name:      "client_secret",
}

var runtimeAuthenticationSecretContext = secrets.Context{
	Namespace: "auth.runtime",
	OwnerID:   secrets.OwnerID{15: 2},
	Name:      "client_secret",
}

func (r *SetupWorkflowRepository) ReadWorkflow(ctx context.Context, digest setup.Digest) (setup.WorkflowResult, error) {
	if ctx.Err() != nil {
		return setup.WorkflowResult{}, setup.ErrPersistenceCanceled
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return setup.WorkflowResult{}, setup.ErrPersistenceUnavailable
	}
	var complete bool
	var current setup.Step
	var draft []byte
	var sessionValid bool
	var revision int64
	var evidenceRevision sql.NullInt64
	var evidencePolicy sql.NullInt64
	var evidenceValidatedAt sql.NullTime
	err := r.pool.pool.QueryRow(ctx, `SELECT st.complete, st.current_step, st.draft, st.authentication_revision,
		EXISTS (SELECT 1 FROM setup_sessions s WHERE s.token_digest = $1 AND s.revoked_at IS NULL
			AND s.expires_at > transaction_timestamp()),
		ev.authentication_revision, ev.policy_version, ev.validated_at
		FROM setup_state st
		LEFT JOIN setup_authentication_validation_evidence ev ON ev.singleton = st.singleton
			AND ev.authentication_revision = st.authentication_revision AND ev.policy_version = $2
		WHERE st.singleton = TRUE`, digest[:], setup.AuthenticationValidationPolicyVersion).
		Scan(&complete, &current, &draft, &revision, &sessionValid, &evidenceRevision, &evidencePolicy, &evidenceValidatedAt)
	if err != nil {
		return setup.WorkflowResult{}, classifySetupPersistenceError(ctx, err)
	}
	if !sessionValid {
		return setup.WorkflowResult{Outcome: setup.WorkflowSessionRequired}, nil
	}
	if complete {
		return setup.WorkflowResult{Outcome: setup.WorkflowCompleted}, nil
	}
	result, err := decodeWorkflowState(current, draft)
	if err != nil {
		return setup.WorkflowResult{}, err
	}
	if r.secrets != nil {
		_, secretErr := r.secrets.Read(ctx, setupAuthenticationSecretContext)
		if secretErr == nil {
			result.State.ClientSecretConfigured = true
		} else if !errors.Is(secretErr, ErrEncryptedSecretNotFound) {
			return setup.WorkflowResult{}, setup.ErrPersistenceUnavailable
		}
	}
	result.State.AuthenticationRevision = revision
	if evidenceRevision.Valid && evidencePolicy.Valid && evidenceValidatedAt.Valid {
		result.State.AuthenticationEvidence = &setup.AuthenticationValidationEvidence{
			AuthenticationRevision: evidenceRevision.Int64, PolicyVersion: int(evidencePolicy.Int64), ValidatedAt: evidenceValidatedAt.Time,
		}
	}
	return result, nil
}

func (r *SetupWorkflowRepository) SaveGeneral(ctx context.Context, digest setup.Digest, general setup.General) (result setup.WorkflowResult, err error) {
	if ctx.Err() != nil {
		return result, setup.ErrPersistenceCanceled
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return result, setup.ErrPersistenceUnavailable
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var complete bool
	var current setup.Step
	var existing []byte
	if err = tx.QueryRow(ctx, `SELECT complete, current_step, draft FROM setup_state
		WHERE singleton = TRUE FOR UPDATE`).Scan(&complete, &current, &existing); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	// Mutation precedence is intentionally different from protected reads. Once
	// the serialized setup state is complete, report that terminal state before
	// consulting the session table, whose active rows completion revokes.
	if complete {
		return setup.WorkflowResult{Outcome: setup.WorkflowCompleted}, commitWorkflow(ctx, tx)
	}
	if active, activeErr := runtimeAuthActive(ctx, tx); activeErr != nil {
		return result, activeErr
	} else if active {
		return setup.WorkflowResult{Outcome: setup.WorkflowCompleted}, commitWorkflow(ctx, tx)
	}
	var sessionValid bool
	// clock_timestamp() is sampled by this statement after the setup-state lock
	// is acquired. transaction_timestamp() would preserve a stale pre-wait time.
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM setup_sessions WHERE token_digest = $1
		AND revoked_at IS NULL AND expires_at > clock_timestamp())`, digest[:]).Scan(&sessionValid); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if !sessionValid {
		return setup.WorkflowResult{Outcome: setup.WorkflowSessionRequired}, commitWorkflow(ctx, tx)
	}
	existingState, err := decodeWorkflowState(current, existing)
	if err != nil {
		return result, err
	}
	if existingState.State.Authentication != nil {
		present, presenceErr := r.secretPresentTx(ctx, tx)
		if presenceErr != nil {
			return result, presenceErr
		}
		existingState.State.ClientSecretConfigured = present
	}
	next := current
	if stepOrder(current) < stepOrder(setup.StepAuthentication) {
		next = setup.StepAuthentication
	}
	if current == next && existingState.State.General != nil && *existingState.State.General == general {
		if err = tx.Commit(ctx); err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		existingState.State.Current = next
		return existingState, nil
	}
	draft, err := setup.EncodeWorkflowDraft(setup.WorkflowDraft{General: &general, Authentication: existingState.State.Authentication})
	if err != nil {
		return result, setup.ErrInvalidPersistenceInput
	}
	if _, err = tx.Exec(ctx, `UPDATE setup_state SET current_step = $1, draft = $2, updated_at = transaction_timestamp()
		WHERE singleton = TRUE`, next, draft); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO setup_workflow_events (event_type, created_at)
		VALUES ('general_saved', clock_timestamp())`); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	existingState.State.Current = next
	existingState.State.General = &general
	return existingState, nil
}

func (r *SetupWorkflowRepository) SaveAuthentication(ctx context.Context, digest setup.Digest, authentication setup.Authentication, operation setup.SecretOperation) (result setup.WorkflowResult, err error) {
	if ctx.Err() != nil {
		return result, setup.ErrPersistenceCanceled
	}
	if r == nil || r.pool == nil || r.pool.pool == nil || r.secrets == nil {
		return result, setup.ErrPersistenceUnavailable
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var complete bool
	var current setup.Step
	var existing []byte
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT complete,current_step,draft,authentication_revision FROM setup_state WHERE singleton=TRUE FOR UPDATE`).
		Scan(&complete, &current, &existing, &revision); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if complete {
		return setup.WorkflowResult{Outcome: setup.WorkflowCompleted}, commitWorkflow(ctx, tx)
	}
	if active, activeErr := runtimeAuthActive(ctx, tx); activeErr != nil {
		return result, activeErr
	} else if active {
		return setup.WorkflowResult{Outcome: setup.WorkflowCompleted}, commitWorkflow(ctx, tx)
	}
	var sessionValid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM setup_sessions WHERE token_digest=$1
		AND revoked_at IS NULL AND expires_at > clock_timestamp())`, digest[:]).Scan(&sessionValid); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if !sessionValid {
		return setup.WorkflowResult{Outcome: setup.WorkflowSessionRequired}, commitWorkflow(ctx, tx)
	}
	state, err := decodeWorkflowState(current, existing)
	if err != nil {
		return result, err
	}
	envelope, readErr := r.secrets.ReadTx(ctx, tx, setupAuthenticationSecretContext)
	present := readErr == nil
	if readErr != nil && !errors.Is(readErr, ErrEncryptedSecretNotFound) {
		return result, setup.ErrPersistenceUnavailable
	}
	if authentication.ClientType == setup.ClientTypePublic && operation.Kind() != setup.SecretClear {
		return result, setup.ErrInvalidPersistenceInput
	}
	if authentication.ClientType == setup.ClientTypeConfidential && operation.Kind() == setup.SecretClear {
		return result, setup.ErrInvalidPersistenceInput
	}
	if authentication.ClientType == setup.ClientTypeConfidential && operation.Kind() == setup.SecretPreserve && !present {
		return result, setup.ErrInvalidPersistenceInput
	}
	secretChanged := false
	switch operation.Kind() {
	case setup.SecretPreserve:
	case setup.SecretClear:
		if present {
			if _, err = r.secrets.DeleteTx(ctx, tx, setupAuthenticationSecretContext); err != nil {
				return result, setup.ErrPersistenceUnavailable
			}
			secretChanged, present = true, false
		}
	case setup.SecretReplace:
		if r.cipher == nil {
			return setup.WorkflowResult{Outcome: setup.WorkflowLocked}, commitWorkflow(ctx, tx)
		}
		if present {
			plaintext, decryptErr := r.cipher.Decrypt(setupAuthenticationSecretContext, envelope)
			if decryptErr != nil {
				return setup.WorkflowResult{Outcome: setup.WorkflowLocked}, commitWorkflow(ctx, tx)
			}
			equal := false
			_ = operation.EncryptValue(func(candidate []byte) error {
				equal = len(candidate) == len(plaintext) && subtle.ConstantTimeCompare(candidate, plaintext) == 1
				return nil
			})
			for i := range plaintext {
				plaintext[i] = 0
			}
			if equal {
				break
			}
		}
		var encrypted secrets.Envelope
		if err = operation.EncryptValue(func(plaintext []byte) error {
			var encryptErr error
			encrypted, encryptErr = r.cipher.Encrypt(setupAuthenticationSecretContext, plaintext)
			return encryptErr
		}); err != nil {
			return setup.WorkflowResult{Outcome: setup.WorkflowLocked}, commitWorkflow(ctx, tx)
		}
		if err = r.secrets.UpsertTx(ctx, tx, setupAuthenticationSecretContext, encrypted); err != nil {
			if errors.Is(err, ErrEncryptionKeyRequired) || errors.Is(err, ErrEncryptionKeyIncompatible) {
				return setup.WorkflowResult{Outcome: setup.WorkflowLocked}, commitWorkflow(ctx, tx)
			}
			return result, setup.ErrPersistenceUnavailable
		}
		secretChanged, present = true, true
	default:
		return result, setup.ErrInvalidPersistenceInput
	}
	next := current
	if stepOrder(current) < stepOrder(setup.StepProviders) {
		next = setup.StepProviders
	}
	draftChanged := state.State.Authentication == nil || !setup.AuthenticationEqual(*state.State.Authentication, authentication) || next != current
	if !draftChanged && !secretChanged {
		state.State.ClientSecretConfigured = present
		state.State.AuthenticationRevision = revision
		return state, commitWorkflow(ctx, tx)
	}
	draft, err := setup.EncodeWorkflowDraft(setup.WorkflowDraft{General: state.State.General, Authentication: &authentication})
	if err != nil {
		return result, setup.ErrInvalidPersistenceInput
	}
	revision++
	if _, err = tx.Exec(ctx, `UPDATE setup_state SET current_step=$1,draft=$2,authentication_revision=$3,updated_at=transaction_timestamp() WHERE singleton=TRUE`, next, draft, revision); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM setup_authentication_validation_evidence WHERE singleton=TRUE`); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES ('authentication_saved',clock_timestamp())`); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	return setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{Current: next, General: state.State.General, Authentication: &authentication, ClientSecretConfigured: present, AuthenticationRevision: revision}}, nil
}

// ActivateAuthentication is a local, atomic promotion of a validated setup
// draft. It deliberately makes no outbound OIDC call.
func (r *SetupWorkflowRepository) ActivateAuthentication(ctx context.Context, digest setup.Digest) (setup.ActivationResult, error) {
	if ctx.Err() != nil || r == nil || r.pool == nil || r.pool.pool == nil || r.secrets == nil {
		return setup.ActivationResult{}, setup.ErrPersistenceUnavailable
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return setup.ActivationResult{}, classifySetupPersistenceError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var draft []byte
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT draft,authentication_revision FROM setup_state WHERE singleton=TRUE FOR UPDATE`).Scan(&draft, &revision); err != nil {
		return setup.ActivationResult{}, classifySetupPersistenceError(ctx, err)
	}
	var activeRevision int64
	if err = tx.QueryRow(ctx, `SELECT authentication_revision FROM auth_runtime_activation WHERE singleton=TRUE`).Scan(&activeRevision); err == nil {
		return setup.ActivationResult{Outcome: setup.ActivationAlreadyActive, Revision: activeRevision}, commitActivation(ctx, tx)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return setup.ActivationResult{}, classifySetupPersistenceError(ctx, err)
	}
	var live bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM setup_sessions WHERE token_digest=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp())`, digest[:]).Scan(&live); err != nil {
		return setup.ActivationResult{}, classifySetupPersistenceError(ctx, err)
	}
	if !live {
		return setup.ActivationResult{Outcome: setup.ActivationSessionRequired, Revision: revision}, commitActivation(ctx, tx)
	}
	decoded, err := setup.DecodeWorkflowDraft(draft)
	if err != nil || decoded.General == nil || decoded.Authentication == nil || revision < 1 {
		return setup.ActivationResult{Outcome: setup.ActivationRequired, Revision: revision}, commitActivation(ctx, tx)
	}
	var evidenceRevision int64
	var policy int
	if err = tx.QueryRow(ctx, `SELECT authentication_revision,policy_version FROM setup_authentication_validation_evidence WHERE singleton=TRUE`).Scan(&evidenceRevision, &policy); errors.Is(err, pgx.ErrNoRows) {
		return setup.ActivationResult{Outcome: setup.ActivationRequired, Revision: revision}, commitActivation(ctx, tx)
	} else if err != nil {
		return setup.ActivationResult{}, classifySetupPersistenceError(ctx, err)
	}
	if evidenceRevision != revision || policy != setup.AuthenticationValidationPolicyVersion {
		return setup.ActivationResult{Outcome: setup.ActivationStale, Revision: revision}, commitActivation(ctx, tx)
	}
	authentication := *decoded.Authentication
	envelope, secretErr := r.secrets.ReadTx(ctx, tx, setupAuthenticationSecretContext)
	present := secretErr == nil
	if secretErr != nil && !errors.Is(secretErr, ErrEncryptedSecretNotFound) {
		return setup.ActivationResult{}, setup.ErrPersistenceUnavailable
	}
	if (authentication.ClientType == setup.ClientTypePublic && present) || (authentication.ClientType == setup.ClientTypeConfidential && !present) {
		return setup.ActivationResult{Outcome: setup.ActivationRequired, Revision: revision}, commitActivation(ctx, tx)
	}
	if present {
		if r.cipher == nil {
			return setup.ActivationResult{Outcome: setup.ActivationLocked, Revision: revision}, commitActivation(ctx, tx)
		}
		plain, decryptErr := r.cipher.Decrypt(setupAuthenticationSecretContext, envelope)
		if decryptErr != nil {
			return setup.ActivationResult{Outcome: setup.ActivationLocked, Revision: revision}, commitActivation(ctx, tx)
		}
		runtimeEnvelope, encryptErr := r.cipher.Encrypt(runtimeAuthenticationSecretContext, plain)
		for i := range plain {
			plain[i] = 0
		}
		if encryptErr != nil || r.secrets.UpsertTx(ctx, tx, runtimeAuthenticationSecretContext, runtimeEnvelope) != nil {
			return setup.ActivationResult{Outcome: setup.ActivationLocked, Revision: revision}, commitActivation(ctx, tx)
		}
		if _, err = r.secrets.DeleteTx(ctx, tx, setupAuthenticationSecretContext); err != nil {
			return setup.ActivationResult{}, setup.ErrPersistenceUnavailable
		}
	}
	scopes, _ := json.Marshal(authentication.Scopes)
	groups, _ := json.Marshal(authentication.GroupRoles)
	var activatedAt time.Time
	if err = tx.QueryRow(ctx, `INSERT INTO auth_runtime_config(singleton,authentication_revision,issuer,client_id,client_type,scopes,groups_claim,default_role,group_role_mappings,activated_at) VALUES(TRUE,$1,$2,$3,$4,$5,$6,$7,$8,clock_timestamp()) RETURNING activated_at`, revision, authentication.Issuer, authentication.ClientID, authentication.ClientType, scopes, authentication.GroupsClaim, authentication.DefaultRole, groups).Scan(&activatedAt); err != nil {
		return setup.ActivationResult{}, classifySetupPersistenceError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_runtime_activation(singleton,authentication_revision,activated_at) VALUES(TRUE,$1,$2)`, revision, activatedAt); err != nil {
		return setup.ActivationResult{}, classifySetupPersistenceError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES('authentication_activated',clock_timestamp())`); err != nil {
		return setup.ActivationResult{}, classifySetupPersistenceError(ctx, err)
	}
	id, err := activationAuditID()
	if err != nil {
		return setup.ActivationResult{}, setup.ErrPersistenceUnavailable
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_audit_events(id,event_type,data,created_at) VALUES($1,'configuration_activated','{}'::jsonb,clock_timestamp())`, id); err != nil {
		return setup.ActivationResult{}, classifySetupPersistenceError(ctx, err)
	}
	if err = r.verifyActivatedRuntimeTx(ctx, tx); err != nil {
		return setup.ActivationResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return setup.ActivationResult{}, classifySetupPersistenceError(ctx, err)
	}
	return setup.ActivationResult{Outcome: setup.ActivationSucceeded, Revision: revision}, nil
}

func commitActivation(ctx context.Context, tx pgx.Tx) error { return commitWorkflow(ctx, tx) }

// verifyActivatedRuntimeTx proves that the exact snapshot written by this
// transaction can be decoded and, for confidential clients, that only its
// runtime-bound secret decrypts. The transaction is rolled back on failure.
func (r *SetupWorkflowRepository) verifyActivatedRuntimeTx(ctx context.Context, tx pgx.Tx) error {
	var config auth.RuntimeConfig
	var scopes, mappings []byte
	if err := tx.QueryRow(ctx, `SELECT authentication_revision,issuer,client_id,client_type,scopes,groups_claim,default_role,group_role_mappings,activated_at
		FROM auth_runtime_config WHERE singleton=TRUE`).Scan(&config.AuthenticationRevision, &config.Issuer, &config.ClientID, &config.ClientType, &scopes, &config.GroupsClaim, &config.DefaultRole, &mappings, &config.ActivatedAt); err != nil {
		return classifySetupPersistenceError(ctx, err)
	}
	if len(scopes) > 2048 || len(mappings) > 16384 || json.Unmarshal(scopes, &config.Scopes) != nil {
		return setup.ErrInvalidPersistenceInput
	}
	var stored []struct {
		Group string `json:"group"`
		Role  string `json:"role"`
	}
	if json.Unmarshal(mappings, &stored) != nil {
		return setup.ErrInvalidPersistenceInput
	}
	config.GroupRoles = make([]auth.GroupRoleMapping, len(stored))
	for i, mapping := range stored {
		config.GroupRoles[i] = auth.GroupRoleMapping{Group: mapping.Group, Role: auth.Role(mapping.Role)}
	}
	if err := config.Validate(); err != nil {
		return setup.ErrInvalidPersistenceInput
	}
	if config.ClientType == string(setup.ClientTypePublic) {
		return nil
	}
	if r.cipher == nil {
		return setup.ErrPersistenceUnavailable
	}
	envelope, err := r.secrets.ReadTx(ctx, tx, runtimeAuthenticationSecretContext)
	if err != nil {
		return setup.ErrPersistenceUnavailable
	}
	plaintext, err := r.cipher.Decrypt(runtimeAuthenticationSecretContext, envelope)
	if err != nil {
		return setup.ErrPersistenceUnavailable
	}
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()
	if len(plaintext) == 0 || len(plaintext) > setup.AuthenticationClientSecretMax || !utf8.Valid(plaintext) || bytes.IndexFunc(plaintext, unicode.IsControl) >= 0 {
		return setup.ErrInvalidPersistenceInput
	}
	return nil
}

func activationAuditID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func runtimeAuthActive(ctx context.Context, tx pgx.Tx) (bool, error) {
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM auth_runtime_activation WHERE singleton=TRUE)`).Scan(&active); err != nil {
		return false, classifySetupPersistenceError(ctx, err)
	}
	return active, nil
}

func (r *SetupWorkflowRepository) ReserveAuthenticationValidation(ctx context.Context, sessionDigest, globalDigest setup.Digest, requestedRevision int64) (result setup.AuthenticationValidationSnapshot, err error) {
	if ctx.Err() != nil || r == nil || r.pool == nil || r.pool.pool == nil {
		return result, setup.ErrPersistenceUnavailable
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var complete bool
	var draft []byte
	var revision int64
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT complete,draft,authentication_revision,clock_timestamp() FROM setup_state WHERE singleton=TRUE FOR UPDATE`).
		Scan(&complete, &draft, &revision, &now); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if complete {
		return setup.AuthenticationValidationSnapshot{Outcome: setup.AuthenticationValidationCompleted, Revision: revision}, commitWorkflow(ctx, tx)
	}
	var sessionValid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM setup_sessions WHERE token_digest=$1 AND revoked_at IS NULL AND expires_at > clock_timestamp())`, sessionDigest[:]).Scan(&sessionValid); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if !sessionValid {
		return setup.AuthenticationValidationSnapshot{Outcome: setup.AuthenticationValidationSessionRequired, Revision: revision}, commitWorkflow(ctx, tx)
	}
	decoded, decodeErr := setup.DecodeWorkflowDraft(draft)
	if decodeErr != nil || decoded.Authentication == nil {
		return setup.AuthenticationValidationSnapshot{Outcome: setup.AuthenticationValidationRequired, Revision: revision}, commitWorkflow(ctx, tx)
	}
	if requestedRevision != revision || revision <= 0 {
		return setup.AuthenticationValidationSnapshot{Outcome: setup.AuthenticationValidationStale, Revision: revision}, commitWorkflow(ctx, tx)
	}
	window := now.UTC().Truncate(5 * time.Minute)
	retry := window.Add(5 * time.Minute).Sub(now)
	limited, rateErr := reserveValidationRate(ctx, tx, "session", sessionDigest, window, 5)
	if rateErr != nil {
		return result, rateErr
	}
	globalLimited := false
	if !limited {
		globalLimited, rateErr = reserveValidationRate(ctx, tx, "global", globalDigest, window, 50)
		if rateErr != nil {
			return result, rateErr
		}
	}
	if limited || globalLimited {
		if _, err = tx.Exec(ctx, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES('authentication_validation_rate_limited',clock_timestamp())`); err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		return setup.AuthenticationValidationSnapshot{Outcome: setup.AuthenticationValidationRateLimited, Revision: revision, RetryAfter: retry}, commitWorkflow(ctx, tx)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES('authentication_validation_reserved',clock_timestamp())`); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	return setup.AuthenticationValidationSnapshot{Outcome: setup.AuthenticationValidationSucceeded, Authentication: *decoded.Authentication, Revision: revision}, nil
}

func reserveValidationRate(ctx context.Context, tx pgx.Tx, scope string, digest setup.Digest, window time.Time, limit int) (bool, error) {
	var count int
	err := tx.QueryRow(ctx, `INSERT INTO setup_authentication_validation_rate_limits(scope,key_digest,window_start,attempt_count,updated_at)
		VALUES($1,$2,$3,1,clock_timestamp()) ON CONFLICT(scope,key_digest,window_start) DO UPDATE
		SET attempt_count=setup_authentication_validation_rate_limits.attempt_count+1,updated_at=clock_timestamp()
		WHERE setup_authentication_validation_rate_limits.attempt_count < $4 RETURNING attempt_count`, scope, digest[:], window, limit).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, classifySetupPersistenceError(ctx, err)
	}
	return false, nil
}

func (r *SetupWorkflowRepository) FinishAuthenticationValidation(ctx context.Context, sessionDigest setup.Digest, snapshot setup.AuthenticationValidationSnapshot, evidence *oidc.Evidence, failureKind oidc.FailureKind) (result setup.AuthenticationValidationResult, err error) {
	if ctx.Err() != nil || r == nil || r.pool == nil || r.pool.pool == nil {
		return result, setup.ErrPersistenceUnavailable
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var complete bool
	var draft []byte
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT complete,draft,authentication_revision FROM setup_state WHERE singleton=TRUE FOR UPDATE`).Scan(&complete, &draft, &revision); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if complete {
		return setup.AuthenticationValidationResult{Outcome: setup.AuthenticationValidationCompleted, Revision: revision}, commitWorkflow(ctx, tx)
	}
	var sessionValid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM setup_sessions WHERE token_digest=$1 AND revoked_at IS NULL AND expires_at > clock_timestamp())`, sessionDigest[:]).Scan(&sessionValid); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	decoded, decodeErr := setup.DecodeWorkflowDraft(draft)
	if !sessionValid || decodeErr != nil || decoded.Authentication == nil || revision != snapshot.Revision ||
		!setup.AuthenticationEqual(*decoded.Authentication, snapshot.Authentication) {
		if _, err = tx.Exec(ctx, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES('authentication_validation_stale',clock_timestamp())`); err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		return setup.AuthenticationValidationResult{Outcome: setup.AuthenticationValidationStale, Revision: revision}, commitWorkflow(ctx, tx)
	}
	if evidence == nil {
		outcome, event := validationFailureOutcome(failureKind)
		if _, err = tx.Exec(ctx, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES($1,clock_timestamp())`, event); err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		return setup.AuthenticationValidationResult{Outcome: outcome, Revision: revision}, commitWorkflow(ctx, tx)
	}
	if evidence.PolicyVersion != setup.AuthenticationValidationPolicyVersion || evidence.Issuer != snapshot.Authentication.Issuer {
		return result, setup.ErrInvalidPersistenceInput
	}
	digestBytes, decodeDigestErr := hex.DecodeString(evidence.MetadataDigest)
	if decodeDigestErr != nil || len(digestBytes) != 32 {
		return result, setup.ErrInvalidPersistenceInput
	}
	var existingRevision int64
	var existingPolicy int
	var existingDigest []byte
	var validatedAt time.Time
	err = tx.QueryRow(ctx, `SELECT authentication_revision,policy_version,metadata_digest,validated_at FROM setup_authentication_validation_evidence WHERE singleton=TRUE`).
		Scan(&existingRevision, &existingPolicy, &existingDigest, &validatedAt)
	if err == nil {
		if existingRevision != revision || existingPolicy != evidence.PolicyVersion || subtle.ConstantTimeCompare(existingDigest, digestBytes) != 1 {
			return result, setup.ErrPersistenceUnavailable
		}
		persisted := &setup.AuthenticationValidationEvidence{AuthenticationRevision: revision, PolicyVersion: existingPolicy, ValidatedAt: validatedAt}
		return setup.AuthenticationValidationResult{Outcome: setup.AuthenticationValidationSucceeded, Revision: revision, Evidence: persisted}, commitWorkflow(ctx, tx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO setup_authentication_validation_evidence(singleton,authentication_revision,policy_version,metadata_digest,validated_at)
		VALUES(TRUE,$1,$2,$3,clock_timestamp()) RETURNING validated_at`, revision, evidence.PolicyVersion, digestBytes).Scan(&validatedAt); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES('authentication_validation_succeeded',clock_timestamp())`); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	persisted := &setup.AuthenticationValidationEvidence{AuthenticationRevision: revision, PolicyVersion: evidence.PolicyVersion, ValidatedAt: validatedAt}
	return setup.AuthenticationValidationResult{Outcome: setup.AuthenticationValidationSucceeded, Revision: revision, Evidence: persisted}, nil
}

func validationFailureOutcome(kind oidc.FailureKind) (setup.AuthenticationValidationOutcome, string) {
	switch kind {
	case oidc.FailureRequired, oidc.FailureMetadata:
		return setup.AuthenticationValidationRequired, "authentication_validation_failed_required"
	case oidc.FailureDestination:
		return setup.AuthenticationValidationDestination, "authentication_validation_failed_destination"
	case oidc.FailureUnreachable:
		return setup.AuthenticationValidationUnreachable, "authentication_validation_failed_unreachable"
	default:
		return setup.AuthenticationValidationUnavailable, "authentication_validation_failed_unavailable"
	}
}

func decodeWorkflowState(current setup.Step, draft []byte) (setup.WorkflowResult, error) {
	if !current.Valid() {
		return setup.WorkflowResult{}, setup.ErrStateInvalid
	}
	decoded, err := setup.DecodeWorkflowDraft(draft)
	if err != nil {
		return setup.WorkflowResult{}, err
	}
	return setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{Current: current, General: decoded.General, Authentication: decoded.Authentication}}, nil
}

func (r *SetupWorkflowRepository) secretPresentTx(ctx context.Context, tx pgx.Tx) (bool, error) {
	if r.secrets == nil {
		return false, setup.ErrPersistenceUnavailable
	}
	_, err := r.secrets.ReadTx(ctx, tx, setupAuthenticationSecretContext)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrEncryptedSecretNotFound) {
		return false, nil
	}
	return false, setup.ErrPersistenceUnavailable
}

func commitWorkflow(ctx context.Context, tx pgx.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return classifySetupPersistenceError(ctx, err)
	}
	return nil
}

func stepOrder(step setup.Step) int {
	steps := []setup.Step{setup.StepWelcome, setup.StepGeneral, setup.StepAuthentication, setup.StepProviders,
		setup.StepLibraries, setup.StepNotifications, setup.StepPolicy, setup.StepReview, setup.StepFinish}
	for index, candidate := range steps {
		if candidate == step {
			return index
		}
	}
	return -1
}
